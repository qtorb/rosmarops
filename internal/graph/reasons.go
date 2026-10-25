package graph

// Coverage reason codes. Every coverage.sources_failed reason has the form
// "<code>: <detail>": code is one of these stable values; detail is technical
// (path, Go error, limit) and is never translated. The graph carries no
// user-facing prose; reports map codes to text through the message catalog.
const (
	// ReasonSourceUnsupported: a known source RosmarOps has no reader for yet.
	ReasonSourceUnsupported = "source_unsupported"
	// ReasonReadError: the file exists but could not be read.
	ReasonReadError = "read_error"
	// ReasonParseError: the file was read but is not valid for its format.
	ReasonParseError = "parse_error"
	// ReasonSizeLimitExceeded: the file is over the content-scan size limit
	// (invariant 6) and was not opened.
	ReasonSizeLimitExceeded = "size_limit_exceeded"
	// ReasonFileCountLimitExceeded: a repository holds more candidate files
	// than the per-repo limit (decision 2026-10-24, D2); the rest were not
	// read. Detail: the repository.
	ReasonFileCountLimitExceeded = "file_count_limit_exceeded"
)

// ReasonCodes lists every coverage reason code.
var ReasonCodes = []string{
	ReasonSourceUnsupported,
	ReasonReadError,
	ReasonParseError,
	ReasonSizeLimitExceeded,
	ReasonFileCountLimitExceeded,
}

// Reason formats a coverage reason as "<code>: <detail>".
func Reason(code, detail string) string {
	return code + ": " + detail
}
