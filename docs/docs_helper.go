package docs

import "encoding/json"

// readDoc dapat diganti pada test.
var readDoc = SwaggerInfo.ReadDoc

// GetSwaggerJSON mengembalikan spesifikasi OpenAPI dengan host dan skema sesuai pemanggil,
// sehingga "Try it out" di Swagger UI selalu menembak alamat yang benar.
func GetSwaggerJSON(host, scheme string) ([]byte, error) {
	var spec map[string]any
	if err := json.Unmarshal([]byte(readDoc()), &spec); err != nil {
		return nil, err
	}
	spec["host"] = host
	spec["schemes"] = []string{scheme}
	return json.Marshal(spec)
}
