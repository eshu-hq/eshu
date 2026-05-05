package contentrefs

import (
	"reflect"
	"testing"
)

func TestServiceNamesExtractsHyphenatedServiceReferences(t *testing.T) {
	t.Parallel()

	got := ServiceNames(`
service: sample-service-api
url: https://sample-service-api.qa.example.test/v1
run: helm upgrade --install sample-service-api ./charts/sample-service-api
`)
	want := []string{"sample-service-api"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ServiceNames() = %#v, want %#v", got, want)
	}
}

func TestServiceNamesSkipsCommonNonServiceTokens(t *testing.T) {
	t.Parallel()

	got := ServiceNames(`
styles:
  font-size: 12px
  margin-top: 1rem
  background-color: #fff
headers:
  content-type: application/json
serviceAccount:
  name: service-role
permissions:
  pull-requests: write
metadata:
  max-old-space-size: "512"
`)
	if len(got) != 0 {
		t.Fatalf("ServiceNames() = %#v, want no common config tokens", got)
	}
}
