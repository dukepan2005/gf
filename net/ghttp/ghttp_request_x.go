package ghttp

import "net/http"

// NewTestRequest creates and returns a new public request object， only for test middleware
func NewTestRequest(s *Server, r *http.Request, w http.ResponseWriter) *Request {
	return newRequest(s, r, w)
}

// SetHandlerResponse set response for TestRequest
func (r *Request) SetHandlerResponse(resp interface{}) {
	r.handlerResponse = resp
}
