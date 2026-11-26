package gen

import "testing"

func TestOperator_Adept(t *testing.T) {
	docs, err := Operator("testdata/operator")
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 2 {
		t.Fatalf("want 2 CRDs (non-CRD docs skipped), got %d", len(docs))
	}
	if docs[0].Kind != "Action" || docs[1].Kind != "MaintenanceWindow" {
		t.Errorf("kinds not sorted: %s, %s", docs[0].Kind, docs[1].Kind)
	}
	a := docs[0]
	if a.Group != "adept.helmetica.io" || a.Scope != "Namespaced" || len(a.Versions) != 1 || a.Versions[0].Name != "v1" {
		t.Errorf("action = %+v", a)
	}
	if _, ok := field(a.Versions[0].Spec, "spec.type"); !ok {
		t.Errorf("spec.type missing")
	}
	if _, ok := field(a.Versions[0].Status, "status.phase"); !ok {
		t.Errorf("status.phase missing")
	}
}

func TestOperator_NoCRDDir(t *testing.T) {
	docs, err := Operator(t.TempDir())
	if err != nil || docs != nil {
		t.Fatalf("want nil, nil; got %v, %v", docs, err)
	}
}
