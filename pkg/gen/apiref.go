package gen

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"sigs.k8s.io/yaml"

	"github.com/helmetica-framework/rosarium/pkg/schemadoc"
)

// CRDDir is where kubebuilder operators keep their generated CRDs.
const CRDDir = "config/crd/bases"

// CRDDoc documents one CustomResourceDefinition.
type CRDDoc struct {
	Group, Kind, Scope string
	Versions           []CRDVersionDoc
}

// CRDVersionDoc documents one served version of a CRD.
type CRDVersionDoc struct {
	Name         string
	Description  string
	Spec, Status []schemadoc.Field
}

// Operator reads repoDir/config/crd/bases/*.yaml. A missing directory
// yields nil, nil; files that are not CRDs are skipped.
func Operator(repoDir string) ([]CRDDoc, error) {
	files, err := filepath.Glob(filepath.Join(repoDir, CRDDir, "*.yaml"))
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		if _, err := os.Stat(filepath.Join(repoDir, CRDDir)); errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
	}
	var docs []CRDDoc
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		for _, part := range bytes.Split(raw, []byte("\n---")) {
			var crd apiextv1.CustomResourceDefinition
			if err := yaml.Unmarshal(part, &crd); err != nil {
				return nil, fmt.Errorf("%s: %w", f, err)
			}
			if crd.Kind != "CustomResourceDefinition" {
				continue
			}
			docs = append(docs, crdDoc(crd))
		}
	}
	sort.Slice(docs, func(i, j int) bool { return docs[i].Kind < docs[j].Kind })
	return docs, nil
}

func crdDoc(crd apiextv1.CustomResourceDefinition) CRDDoc {
	d := CRDDoc{Group: crd.Spec.Group, Kind: crd.Spec.Names.Kind, Scope: string(crd.Spec.Scope)}
	for _, v := range crd.Spec.Versions {
		if !v.Served {
			continue
		}
		vd := CRDVersionDoc{Name: v.Name}
		if v.Schema != nil && v.Schema.OpenAPIV3Schema != nil {
			root := v.Schema.OpenAPIV3Schema
			vd.Description = root.Description
			vd.Spec = schemadoc.Flatten(root.Properties["spec"], "spec")
			vd.Status = schemadoc.Flatten(root.Properties["status"], "status")
		}
		d.Versions = append(d.Versions, vd)
	}
	return d
}
