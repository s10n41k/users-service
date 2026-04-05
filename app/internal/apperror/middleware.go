package apperror

import (
	"errors"
	"net/http"
)

type Handler func(http.ResponseWriter, *http.Request) error

// appHandler — псевдоним для обратной совместимости внутри пакета
type appHandler = Handler

func Middleware(handler Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var appErr *AppError

		err := handler(w, r)
		if err != nil {
			if errors.As(err, &appErr) {
				if errors.Is(err, ErrNotFound) {
					w.WriteHeader(http.StatusNotFound)
					w.Write(ErrNotFound.Marshal())
					return
				}
				err = err.(*AppError)
				w.WriteHeader(http.StatusBadRequest)
				w.Write(appErr.Marshal())
				return
			}
			w.WriteHeader(http.StatusTeapot)
			w.Write(systemError(err).Marshal())

		}
	}
}

func Raw(handler Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		err := handler(w, r)
		if err != nil {
			// Просто пробрасываем исходную ошибку в ответ
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
}
