package collateraldomain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Collateral struct {
	Revision             int64  `json:"revision"`
	Type                 string `json:"type" enums:"real_estate,equipment,vehicle" binding:"required"`
	Ownership            string `json:"ownership" enums:"existing,acquired" binding:"required"`
	Description          string `json:"description" binding:"required"`
	DeclaredValueKopecks int64  `json:"declared_value_kopecks" minimum:"1" binding:"required"`
}

func (c Collateral) Validate() error {
	if c.Type != "real_estate" && c.Type != "equipment" && c.Type != "vehicle" {
		return fmt.Errorf("invalid collateral type")
	}
	if c.Ownership != "existing" && c.Ownership != "acquired" {
		return fmt.Errorf("invalid collateral ownership")
	}
	if strings.TrimSpace(c.Description) == "" || c.DeclaredValueKopecks <= 0 {
		return fmt.Errorf("description and positive declared value are required")
	}
	return nil
}

// Hash включает только предмет залога. Изменение суммы кредита его не меняет.
func (c Collateral) Hash() string {
	// Служебная версия не является клиентским полем предмета залога.
	data, _ := json.Marshal(struct {
		Type                 string
		Ownership            string
		Description          string
		DeclaredValueKopecks int64
	}{c.Type, c.Ownership, c.Description, c.DeclaredValueKopecks})
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

type CollateralReview struct {
	ID                   uuid.UUID `json:"id"`
	ApplicationID        uuid.UUID `json:"application_id"`
	Revision             int64     `json:"revision"`
	Version              int64     `json:"version"`
	Reviewer             uuid.UUID `json:"reviewer"`
	CreatedAt            time.Time `json:"created_at"`
	CollateralHash       string    `json:"collateral_hash"`
	CollateralRevision   int64     `json:"collateral_revision"`
	Result               string    `json:"result"`
	AcceptedValueKopecks int64     `json:"accepted_value_kopecks"`
	Comment              string    `json:"-"` // внутренний комментарий не попадает в клиентский JSON
	PublicComment        string    `json:"public_comment,omitempty"`
}

func (r CollateralReview) Current(c Collateral, now time.Time) bool {
	return r.Result == "accepted" && r.AcceptedValueKopecks > 0 && r.CollateralRevision == c.Revision && r.CollateralHash == c.Hash() &&
		!now.Before(r.CreatedAt) && !now.After(r.CreatedAt.Add(30*24*time.Hour))
}

// CollateralCovers проверяет amount <= floor(acceptedValue*70/100) без float и переполнения.
func CollateralCovers(amount, acceptedValue int64) bool {
	return amount > 0 && acceptedValue > 0 && amount <= acceptedValue/10*7+acceptedValue%10*7/10
}

func CollateralRequired(productCode string, chosen bool) bool {
	return productCode == "equipment" || productCode == "start" || productCode == "investment" || (productCode == "working_capital" && chosen)
}
