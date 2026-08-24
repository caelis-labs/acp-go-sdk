package acp

const (
	// WireProtocolVersion is the stable ACP wire protocol major implemented by
	// the root package.
	WireProtocolVersion = 1

	// SchemaArtifactVersion is the exact official JSON Schema release used to
	// generate the stable root package.
	SchemaArtifactVersion = "1.21.0"

	// SchemaTag and SchemaCommit identify the immutable upstream source.
	SchemaTag    = "schema-v1.21.0"
	SchemaCommit = "272bf799f35a258c6a4107a0410ed361e83683d3"
)
