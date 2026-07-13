package client

import (
	"regexp"
	"strings"
)

// facilityURNPrefix is the form every facility path parameter accepts in place of
// the platform's own id.
const facilityURNPrefix = "urn:fft:facility:tenantFacilityId:"

// uuid matches the platform's ids. Facilities are addressed either by one of these
// or by the tenant's own id wrapped in a URN, and the two are told apart by shape —
// there is no other signal.
var uuid = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// FacilityRef turns whatever the user typed into something a facility path
// parameter accepts.
//
// A platform UUID passes through. Anything else is taken to be the tenant's own id
// and wrapped as urn:fft:facility:tenantFacilityId:<id>, which every facility path
// parameter in the API also accepts. So `fft facility get BER-01` works, and the
// user never has to look up a UUID for an id they chose themselves.
//
// A value that is already a URN passes through too, so that piping one command's
// output into another cannot double-wrap it.
func FacilityRef(id string) string {
	id = strings.TrimSpace(id)

	switch {
	case id == "":
		return ""
	case uuid.MatchString(id):
		return id
	case strings.HasPrefix(strings.ToLower(id), "urn:"):
		return id
	default:
		return facilityURNPrefix + id
	}
}

// The two names a facility goes by outside a path parameter: the platform's
// reference, and the tenant's own id (swagger:38576, 82981).
const (
	KeyFacilityRef      = "facilityRef"
	KeyTenantFacilityID = "tenantFacilityId"
)

// FacilitySelector names a facility the way a request *body* and a search *query*
// want it: as one of two differently-spelled fields, rather than as the URN
// [FacilityRef] builds for a path.
//
// The two are told apart the same way FacilityRef tells them apart — by shape,
// because there is no other signal. A platform UUID (or an already-built URN) is a
// facilityRef; anything else is the tenant's own id. key is "" for an empty id.
//
// The difference from the URN wrap is not cosmetic. A stock creation body carrying
// {"facilityRef": "BER-01"} is refused, because BER-01 is not a reference; a
// search query filtering facilityRef by "BER-01" is *accepted* and quietly matches
// nothing, which is worse. Same value, same intent, two spellings — not something
// each command should be left to work out for itself.
func FacilitySelector(id string) (key, value string) {
	id = strings.TrimSpace(id)

	switch {
	case id == "":
		return "", ""
	case uuid.MatchString(id), strings.HasPrefix(strings.ToLower(id), "urn:"):
		return KeyFacilityRef, id
	default:
		return KeyTenantFacilityID, id
	}
}
