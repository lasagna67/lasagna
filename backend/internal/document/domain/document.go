// Package documentdomain — файлы заявки: договоры и другие документы. Файл
// лежит в S3, метаданные — в application_documents.
package documentdomain

import (
	"time"

	"github.com/google/uuid"
)

type DocumentType string

const DocumentLoanAgreement DocumentType = "loan_agreement"

// Document — файл заявки. StorageKey внутренний: наружу файл отдаётся только
// через авторизованное чтение.
type Document struct {
	ID            uuid.UUID
	ApplicationID uuid.UUID
	Type          DocumentType
	FileName      string
	ContentType   string
	SizeBytes     int64
	StorageKey    string
	SHA256        string
	CreatedAt     time.Time
}

// Viewer — кто читает документ: участник компании-заёмщика или сотрудник банка.
type Viewer struct {
	UserID uuid.UUID
	Staff  bool
}
