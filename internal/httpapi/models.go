package httpapi

// Внутренняя ошибка чтения запроса и формат ошибки HTTP API.
type apiError struct {
	status  int
	code    string
	message string
}

type errorBody struct {
	Error struct {
		Code    string `json:"code"`
		Field   string `json:"field,omitempty"`
		Message string `json:"message"`
	} `json:"error"`
}
