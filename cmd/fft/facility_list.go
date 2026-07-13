package main

import (
	"encoding/json"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/api"
	"github.com/Joessst-Dev/fft-cli/internal/client"
)

const facilityListLong = `List the facilities of your tenant.

This is POST /api/facilities/search with a cursor: it returns whole facilities
and pages properly, which the legacy GET list does not — that one returns a
reduced projection and cannot express these filters.

By default you get the first page. --all follows the cursor to the end, and says
so on stderr if it stops early rather than pretending it reached it.

  fft facility list --status ONLINE --type SUPPLIER
  fft facility list --all --size 250 -o json | jq -r '.[].tenantFacilityId'
  fft facility list --sort name:asc --total

stdout carries the facilities and nothing else. The total, the truncation notice
and every other remark go to stderr, so a pipe into jq is never contaminated.`

// facilitySortFields are the fields the API will sort by. The search API accepts
// exactly one — not zero of them, and not two.
var facilitySortFields = []string{"id", "name", "status", "type", "tenantFacilityId", "locationType", "lastModified"}

func newFacilityListCmd(deps *Deps) *cobra.Command {
	var (
		status   []string
		typ      string
		tenantID string
		sortBy   string
		page     pageFlags
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List facilities",
		Long:  facilityListLong,
		Args:  usageArgs(cobra.NoArgs),

		Annotations: map[string]string{annotationOperationID: "searchFacility"},

		RunE: func(cmd *cobra.Command, _ []string) error {
			query, err := facilityQuery(status, typ, tenantID)
			if err != nil {
				return err
			}

			payload := client.FacilitySearchPayload{Query: query}
			if payload.Sort, err = facilitySort(sortBy); err != nil {
				return err
			}

			return runSearch(cmd, deps, client.FacilitySearch[json.RawMessage](), staticQuery(payload), page, facilityList())
		},
	}

	f := cmd.Flags()
	f.StringSliceVar(&status, "status", nil,
		"Only facilities in these states: "+strings.Join(facilityStatuses(), ", "))
	f.StringVar(&typ, "type", "",
		"Only facilities of this type: "+strings.Join(facilityTypes(), " or "))
	f.StringVar(&tenantID, "tenant-facility-id", "", "Only the facility with this tenantFacilityId")
	f.StringVar(&sortBy, "sort", "",
		"Sort by one field, as field:asc or field:desc ("+strings.Join(facilitySortFields, ", ")+")")
	page.register(f, "facilities")

	registerEnumCompletion(cmd, "status", facilityStatuses())
	registerEnumCompletion(cmd, "type", facilityTypes())

	return cmd
}

// facilityQuery turns the filter flags into the API's query. Every value is
// checked here, because the API's own answer to an unknown status is a 400 that
// does not name the field it disliked.
func facilityQuery(status []string, typ, tenantID string) (api.FacilitySearchQuery, error) {
	var query api.FacilitySearchQuery

	if len(status) > 0 {
		in := make([]api.FacilityStatusEnumFilterIn, 0, len(status))
		for _, s := range status {
			v, err := enumValue("status", s, facilityStatuses())
			if err != nil {
				return query, err
			}
			in = append(in, api.FacilityStatusEnumFilterIn(v))
		}
		query.Status = &api.FacilityStatusEnumFilter{In: &in}
	}

	if typ != "" {
		v, err := enumValue("type", typ, facilityTypes())
		if err != nil {
			return query, err
		}
		eq := api.FacilityTypeEnumFilterEq(v)
		query.Type = &api.FacilityTypeEnumFilter{Eq: &eq}
	}

	if tenantID != "" {
		query.TenantFacilityId = &api.StringFilter{Eq: &tenantID}
	}

	return query, nil
}

// facilitySort parses --sort into the API's sort object, which is a struct with
// exactly one field set rather than a string.
func facilitySort(v string) ([]api.FacilitySort, error) {
	return parseSort(v, facilitySortFields, func(s *api.FacilitySort, field, dir string) bool {
		switch field {
		case "id":
			s.Id = ptr(api.FacilitySortId(dir))
		case "name":
			s.Name = ptr(api.FacilitySortName(dir))
		case "status":
			s.Status = ptr(api.FacilitySortStatus(dir))
		case "type":
			s.Type = ptr(api.FacilitySortType(dir))
		case "tenantfacilityid":
			s.TenantFacilityId = ptr(api.FacilitySortTenantFacilityId(dir))
		case "locationtype":
			s.LocationType = ptr(api.FacilitySortLocationType(dir))
		case "lastmodified":
			s.LastModified = ptr(api.FacilitySortLastModified(dir))
		default:
			return false
		}
		return true
	})
}
