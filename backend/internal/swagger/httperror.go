// Package swagger — типы-подмены для swag-аннотаций. Описывают ответы,
// которые формирует не наш код (например, echo), поэтому swag не может
// вывести их схему сам. В рантайме не используются.
package swagger

// HTTPError — тело ответа, которое echo отдаёт для *echo.HTTPError.
type HTTPError struct {
	Message string `json:"message" example:"Неверный email или пароль"`
}
