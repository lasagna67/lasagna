package reviewdomain

import "testing"

// Эталоны посчитаны независимо, в точных дробях по той же формуле ТЗ.
func TestAnnuitySchedule(t *testing.T) {
	tests := []struct {
		name                  string
		amount                int64
		rateBps, term         int
		payment, lastPayment  int64
		firstInterest, totInt int64
	}{
		{"1 млн на 12 мес под 21,5%", 100000000, 2150, 12, 9335391, 9335390, 1791667, 12024691},
		{"без процентов: остаток деления в последнем платеже", 10000000, 0, 7, 1428571, 1428574, 0, 0},
		{"10 млрд копеек на 84 мес", 1000000000000, 1850, 84, 21311919193, 21311919193, 15416666667, 790201212212},
		{"500 тыс на 36 мес под 19,9%", 50000000, 1990, 36, 1855633, 1855606, 829167, 16802761},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := AnnuitySchedule(tt.amount, tt.rateBps, tt.term)
			if err != nil {
				t.Fatal(err)
			}
			if len(s) != tt.term {
				t.Fatalf("len = %d, want %d", len(s), tt.term)
			}
			last := s[len(s)-1]
			if s[0].Payment != tt.payment || last.Payment != tt.lastPayment || s[0].Interest != tt.firstInterest {
				t.Errorf("payment %d, last %d, first interest %d; want %d, %d, %d",
					s[0].Payment, last.Payment, s[0].Interest, tt.payment, tt.lastPayment, tt.firstInterest)
			}

			var principal, interest int64
			for i, p := range s {
				principal += p.Principal
				interest += p.Interest
				if p.Month != i+1 || p.Payment != p.Principal+p.Interest {
					t.Errorf("row %d = %+v", i, p)
				}
				if i < len(s)-1 && p.Payment != tt.payment {
					t.Errorf("month %d payment %d; regular %d", p.Month, p.Payment, tt.payment)
				}
			}
			if principal != tt.amount || last.Balance != 0 || interest != tt.totInt {
				t.Errorf("principal %d, balance %d, interest %d; want %d, 0, %d", principal, last.Balance, interest, tt.amount, tt.totInt)
			}
		})
	}
}

func TestAnnuityScheduleRejectsInvalidInput(t *testing.T) {
	for _, tt := range []struct {
		amount        int64
		rateBps, term int
	}{
		{0, 2150, 12}, {-1, 2150, 12}, {100, -1, 12}, {100, 2150, 0}, {100, 2150, maxTermMonths + 1},
	} {
		if _, err := AnnuitySchedule(tt.amount, tt.rateBps, tt.term); err == nil {
			t.Errorf("AnnuitySchedule(%d, %d, %d) = nil error", tt.amount, tt.rateBps, tt.term)
		}
	}
}
