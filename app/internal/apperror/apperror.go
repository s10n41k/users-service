package apperror

import (
	"encoding/json"
	"errors"
)

var (
	ErrNotFound = NewAppError(nil, "not found", "", "RAT-000300")
)

type AppError struct {
	Err              error  `json:"err"`
	Message          string `json:"message"`
	DeveloperMessage string `json:"developer_message"`
	Code             string `json:"code"`
}

func (e *AppError) Error() string {
	return e.Message
}

func (e *AppError) Unwrap() error {
	return e.Err
}

func (e *AppError) Marshal() []byte {
	marshal, err := json.Marshal(e)
	if err != nil {
		return nil
	}
	return marshal
}

func NewAppError(err error, message, developerMessage, code string) *AppError {
	return &AppError{
		Err:              err,
		Message:          message,
		DeveloperMessage: developerMessage,
		Code:             code,
	}
}

func BadRequest(message, developerMessage string) *AppError {
	return NewAppError(errors.New(message), message, developerMessage, "RAT-000001")
}

func systemError(err error) *AppError {
	return NewAppError(nil, "system err", err.Error(), "RAT-000000")
}
