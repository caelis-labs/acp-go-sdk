package acp

const (
	// WireProtocolVersion is the stable ACP wire protocol major implemented by
	// the root package.
	WireProtocolVersion = 1

	// SchemaArtifactVersion is the exact official JSON Schema release used to
	// generate the stable root package.
	SchemaArtifactVersion = "1.27.0"

	// SchemaTag and SchemaCommit identify the immutable upstream source.
	SchemaTag    = "schema-v1.27.0"
	SchemaCommit = "97fcb0f5b88567dd91d585ea6885761365a6aa7b"
)
