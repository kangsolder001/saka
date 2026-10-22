package httpx

import "encoding/json"

// Optional distinguishes a JSON field that is absent from one that is
// explicitly null, which PATCH endpoints need: absent leaves the value alone,
// null clears it.
//
//	type patch struct {
//	    LogoURL httpx.Optional[string] `json:"logo_url"`
//	}
//
// After decoding, Set says the field was present and Value is nil for null.
type Optional[T any] struct {
	Set   bool
	Value *T
}

// UnmarshalJSON implements json.Unmarshaler. It is only called for fields that
// are present in the payload, which is how Set gets its meaning.
func (o *Optional[T]) UnmarshalJSON(b []byte) error {
	o.Set = true
	return json.Unmarshal(b, &o.Value)
}
