package acp

const (
	// WireProtocolVersion is the stable ACP wire protocol major implemented by
	// the root package.
	WireProtocolVersion = 1

	// SchemaArtifactVersion is the exact official JSON Schema release used to
	// generate the stable root package.
	SchemaArtifactVersion = "1.26.0"

	// SchemaTag and SchemaCommit identify the immutable upstream source.
	SchemaTag    = "schema-v1.26.0"
	SchemaCommit = "e2d83349bc9e28a4059ea3acefa1c91c966f74e2"
)
