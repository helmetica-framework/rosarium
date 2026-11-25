package main

import "embed"

// rootDocs are the framework pages (the ROOT component) and the component
// list. Embedded so that `rosarium preview` in a component repo resolves
// xrefs into ROOT and into the other components.
//
//go:embed all:docs components.yaml
var rootDocs embed.FS
