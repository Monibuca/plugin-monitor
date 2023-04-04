package monitor

import (
	"testing"

	"gopkg.in/yaml.v3"
)

var testYaml = `
- time: 2023-03-22T10:00:00Z
  path: live/test
- time: 2023-03-22T12:00:00Z
  path: live/test2
`

func TestYaml(t *testing.T) {
	t.Run(t.Name(), func(t *testing.T) {
		var index []Index
		err := yaml.Unmarshal([]byte(testYaml), &index)
		if err != nil {
			t.Fatal(err)
		}
		t.Log(index)
	})
}
