package apitypes

// EmbeddingRequest is the OpenAI-compatible embeddings request.
type EmbeddingRequest struct {
	Model          string   `json:"model"`
	Input          []string `json:"input"`
	EncodingFormat string   `json:"encoding_format,omitempty"`
	User           string   `json:"user,omitempty"`
}

// EmbeddingResponse is the OpenAI-compatible embeddings response.
type EmbeddingResponse struct {
	Object string          `json:"object"`
	Data   []EmbeddingData `json:"data"`
	Model  string          `json:"model"`
	Usage  Usage           `json:"usage"`
}

// EmbeddingData is a single embedding vector.
type EmbeddingData struct {
	Object    string    `json:"object"`
	Index     int       `json:"index"`
	Embedding []float32 `json:"embedding"`
}

// Model describes a model available through the gateway.
type Model struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

// ModelList is the response for GET /v1/models.
type ModelList struct {
	Object string  `json:"object"`
	Data   []Model `json:"data"`
}

// APIError is the OpenAI-compatible error envelope.
type APIError struct {
	Error APIErrorBody `json:"error"`
}

// APIErrorBody is the inner error object.
type APIErrorBody struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code,omitempty"`
	Param   string `json:"param,omitempty"`
}

// NewAPIError builds an OpenAI-style error envelope.
func NewAPIError(message, typ, code string) APIError {
	return APIError{Error: APIErrorBody{Message: message, Type: typ, Code: code}}
}
