package domain

// ReasoningEffortMapping rewrites one explicit OpenAI/Codex reasoning effort
// value to another before the account policy ceiling is applied.
type ReasoningEffortMapping struct {
	From string `json:"from"`
	To   string `json:"to"`
}
