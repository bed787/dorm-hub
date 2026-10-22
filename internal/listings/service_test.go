package listings

import (
	"strings"
	"testing"
)

func validInput() Input {
	return Input{Kind: "offer", Category: "things", Title: "Настольная лампа", Description: "Работает, отдам в кампусе.", Location: "PETRODVORTSOVY", PriceMode: "unset", Telegram: "https://t.me/student"}
}
func TestListingValidation(t *testing.T) {
	in := validInput()
	if err := Validate(in); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		change func(*Input)
	}{{"missing title", func(i *Input) { i.Title = "" }}, {"missing contacts", func(i *Input) { i.Telegram = "" }}, {"invalid kind", func(i *Input) { i.Kind = "sale" }}, {"missing price", func(i *Input) { i.PriceMode = "price" }}, {"unexpected price", func(i *Input) {
		v := int64(100)
		i.PriceMinor = &v
	}}, {"negative price", func(i *Input) {
		v := int64(-1)
		i.PriceMode = "price"
		i.PriceMinor = &v
	}}}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := validInput()
			c.change(&in)
			if err := Validate(in); err == nil {
				t.Fatal("invalid listing accepted")
			}
		})
	}
}

func TestDistrictValidation(t *testing.T) {
	for _, code := range []string{"VASILEOSTROVSKY", "PETRODVORTSOVY", "NEVSKY"} {
		in := validInput()
		in.Location = code
		if err := Validate(in); err != nil {
			t.Errorf("district %s: %v", code, err)
		}
	}
	for _, code := range []string{"PUNK", "VUNK", "DUNK", "UNSPECIFIED", ""} {
		in := validInput()
		in.Location = code
		if err := Validate(in); err == nil {
			t.Errorf("legacy/invalid district %q accepted", code)
		}
	}
}

func TestDraftValidationAndPublicationBoundary(t *testing.T) {
	draft := Input{Draft: true, Kind: "offer", Title: "Л", PriceMode: "unset", Telegram: "https://t."}
	if err := ValidateDraft(draft); err != nil {
		t.Fatal(err)
	}
	if err := Validate(draft); err == nil {
		t.Fatal("incomplete draft accepted for publication")
	}
	if HasContent(Input{Kind: "offer", PriceMode: "unset", Telegram: "https://t.me/student"}) {
		t.Fatal("prefilled contacts made an empty draft")
	}
	if !HasContent(Input{HasPhotos: true}) {
		t.Fatal("photo-only draft rejected")
	}
	in := validInput()
	in.Telegram = "@student"
	in = clean(in)
	if err := Validate(in); err != nil {
		t.Fatal(err)
	}
	if in.Telegram != "https://t.me/student" {
		t.Fatal(in.Telegram)
	}
}

func TestExternalAddressValidation(t *testing.T) {
	in := validInput()
	in.Location = "EXTERNAL"
	for _, address := range []string{"", "Санкт-Петербург, улица Примерная, 10"} {
		in.Address = address
		if err := Validate(in); err != nil {
			t.Fatal(err)
		}
	}
	dorm := int64(1)
	in.DormID = &dorm
	if err := ValidateDraft(in); err == nil {
		t.Fatal("external listing accepted a dorm")
	}
	in.DormID = nil
	in.Location = "NEVSKY"
	if err := ValidateDraft(in); err == nil {
		t.Fatal("internal listing accepted a custom address")
	}
	in.Location = "EXTERNAL"
	for _, address := range []string{strings.Repeat("Я", 301), "Улица\x00дом"} {
		in.Address = address
		if err := ValidateDraft(in); err == nil {
			t.Fatal("invalid address accepted")
		}
	}
}
