package load

import (
	"encoding/json"
	"testing"
)

func TestDefinitionReadsDeserializeMarkers(t *testing.T) {
	var definition Definition
	if err := json.Unmarshal([]byte(`{"type":"array","x-deserialize-default-on-error":true,"x-deserialize-skip-invalid-items":true}`), &definition); err != nil {
		t.Fatal(err)
	}
	if !definition.DeserializeDefaultOnError || !definition.DeserializeSkipInvalidItems {
		t.Fatalf("deserialize markers lost: %+v", definition)
	}
	if err := json.Unmarshal([]byte(`{"type":"array"}`), &definition); err != nil {
		t.Fatal(err)
	}
	if definition.DeserializeDefaultOnError || definition.DeserializeSkipInvalidItems {
		t.Fatal("absent markers retained stale values")
	}
}
