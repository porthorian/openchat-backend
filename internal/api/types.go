package api

type APIError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}

type requester struct {
	UserUID  string
	DeviceID string
}
