package acp

const (
	// WireProtocolVersion is the stable ACP wire protocol major implemented by
	// the root package.
	WireProtocolVersion = 1

	// SchemaArtifactVersion is the exact official JSON Schema release used to
	// generate the stable root package.
	SchemaArtifactVersion = "1.23.0"

	// SchemaTag and SchemaCommit identify the immutable upstream source.
	SchemaTag    = "schema-v1.23.0"
	SchemaCommit = "6d08f412a7a1370d3cc9a124e3be3d6acf92641e"
)
