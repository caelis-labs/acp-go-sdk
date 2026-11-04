package acp

const (
	// WireProtocolVersion is the stable ACP wire protocol major implemented by
	// the root package.
	WireProtocolVersion = 1

	// SchemaArtifactVersion is the exact official JSON Schema release used to
	// generate the stable root package.
	SchemaArtifactVersion = "1.29.1"

	// SchemaTag and SchemaCommit identify the immutable upstream source.
	SchemaTag    = "schema-v1.29.1"
	SchemaCommit = "0fc18323a9fdad4e5b4a20ec2f1645dde843fd9b"
)
