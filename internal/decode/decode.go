package decode

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
)

// Documents splits a multi-document YAML (or JSON) byte slice and returns each
// non-empty document as an *unstructured.Unstructured.
func Documents(data []byte) ([]*unstructured.Unstructured, error) {
	var result []*unstructured.Unstructured

	decoder := utilyaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)
	for {
		var ext runtime.RawExtension
		if err := decoder.Decode(&ext); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("decoding YAML document: %w", err)
		}

		raw := bytes.TrimSpace(ext.Raw)
		if len(raw) == 0 || string(raw) == "null" {
			continue
		}

		u := &unstructured.Unstructured{}
		if err := json.Unmarshal(raw, u); err != nil {
			return nil, fmt.Errorf("parsing YAML document as unstructured: %w", err)
		}
		if u.GetKind() == "" {
			continue
		}
		result = append(result, u)
	}

	return result, nil
}
