package validation

import (
	"testing"
	"time"

	companydomain "github.com/lasagna67/lasagna.git/internal/company/domain"
)

func TestContractCertificate(t *testing.T) {
	const companyINN, headINN = "7705005410", "770586906621"
	serial := "A1"
	member := companydomain.Membership{Company: companydomain.Company{INN: companyINN}, Member: companydomain.Member{CanSign: true, CertificateSerial: &serial}}
	card := companydomain.RegistryCard{INN: companyINN, EntityType: companydomain.EntityLegal, Status: companydomain.StatusActive, RegistrationDate: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), Head: &companydomain.Head{INN: headINN}}
	cert := companydomain.Certificate{Serial: serial, Type: companydomain.CertificateLegalEntityHead, OwnerINN: headINN, OrganizationINN: companyINN, Status: "valid", ValidFrom: "2026-01-01", ValidTo: "2026-12-31"}
	for _, name := range []string{"allowed", "revoked permission", "other certificate", "other company", "other head", "revoked certificate", "expired certificate", "future certificate", "terminated company", "empty owner", "empty serial"} {
		t.Run(name, func(t *testing.T) {
			m, c, r := member, cert, card
			switch name {
			case "revoked permission":
				m.Member.CanSign = false
			case "other certificate":
				c.Serial = "B2"
			case "other company":
				c.OrganizationINN = "0000000000"
			case "other head":
				c.OwnerINN = "000000000000"
			case "revoked certificate":
				c.Status = "revoked"
			case "expired certificate":
				c.ValidTo = "2026-01-01"
			case "future certificate":
				c.ValidFrom = "2027-01-01"
			case "terminated company":
				r.Status = companydomain.StatusTerminated
			case "empty owner":
				c.OwnerINN = ""
				r.Head = &companydomain.Head{}
			case "empty serial":
				c.Serial = ""
				empty := ""
				m.Member.CertificateSerial = &empty
			}
			err := ContractCertificate(m, r, c, time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC))
			if (err == nil) != (name == "allowed") {
				t.Fatalf("authorization err=%v", err)
			}
		})
	}
}
