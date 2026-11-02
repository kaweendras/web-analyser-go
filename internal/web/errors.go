package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"web-analyser-go/internal/analyzer"
)

type clientError struct {
	Code       string
	Message    string
	StatusCode int
}

func (e clientError) displayMessage() string {
	if e.StatusCode > 0 {
		return fmt.Sprintf("HTTP %d: %s", e.StatusCode, e.Message)
	}
	return e.Message
}

func classifyAnalysisError(err error, hasPartialResult bool) clientError {
	if hasPartialResult && errors.Is(err, context.DeadlineExceeded) {
		return clientError{
			Code:    "partial_timeout",
			Message: "The analysis could not finish in time, but here is what we found.",
		}
	}

	var validationError *analyzer.ValidationError
	if errors.As(err, &validationError) {
		switch validationError.Code {
		case "missing_hostname":
			return clientError{Code: validationError.Code, Message: "Enter a URL that includes a website hostname."}
		case "unsupported_scheme":
			return clientError{Code: validationError.Code, Message: "Only HTTP and HTTPS website addresses are supported."}
		default:
			return clientError{Code: "invalid_url", Message: "Enter a complete website address, for example https://example.com."}
		}
	}

	var fetchError *analyzer.FetchError
	if errors.As(err, &fetchError) {
		return classifyFetchError(fetchError)
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return clientError{
			Code:    "analysis_timeout",
			Message: "The analysis took too long. Please try again.",
		}
	}

	return clientError{
		Code:    "analysis_failed",
		Message: "Something went wrong while analyzing this page. Please try again.",
	}
}

func classifyFetchError(err *analyzer.FetchError) clientError {
	if errors.Is(err.Err, context.DeadlineExceeded) {
		return clientError{Code: "connection_timeout", Message: "The website took too long to respond. Please try again."}
	}

	switch err.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return clientError{Code: "page_access_denied", Message: "This website blocked the analysis request.", StatusCode: err.StatusCode}
	case http.StatusNotFound:
		return clientError{Code: "page_not_found", Message: "We couldn't find that page. Check the address and try again.", StatusCode: err.StatusCode}
	case http.StatusRequestTimeout, http.StatusGatewayTimeout:
		return clientError{Code: "connection_timeout", Message: "The website took too long to respond. Please try again.", StatusCode: err.StatusCode}
	case http.StatusTooManyRequests:
		return clientError{Code: "rate_limited", Message: "The website is receiving too many requests. Wait a moment and try again.", StatusCode: err.StatusCode}
	}

	if err.StatusCode >= http.StatusInternalServerError {
		return clientError{Code: "page_unavailable", Message: "The website is temporarily unavailable. Please try again later.", StatusCode: err.StatusCode}
	}
	if err.StatusCode >= http.StatusBadRequest {
		return clientError{Code: "page_rejected", Message: "The website rejected the request. Check the address and try again.", StatusCode: err.StatusCode}
	}

	return clientError{Code: "connection_failed", Message: "We couldn't connect to that website. Check the address or try again later."}
}
