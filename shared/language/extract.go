package language

import (
	"database/sql"
	"encoding/xml"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/avabot/ava/Godeps/_workspace/src/github.com/jmoiron/sqlx"
	"github.com/avabot/ava/shared/datatypes"
	"github.com/avabot/ava/shared/helpers/address"
)

var regexCurrency = regexp.MustCompile(`\d+\.?\d*`)

// ExtractCurrency returns a pointer to a string to allow a user a simple check
// to see if currency text was found. If the response is nil, no currency was
// found. This API design also maintains consitency when we want to extract and
// return a struct (which should be returned as a pointer).
func ExtractCurrency(s string) (uint64, *string, error) {
	found := regexCurrency.FindString(s)
	if len(found) == 0 {
		return 0, nil, nil
	}
	tmp := strings.Replace(found, ".", "", 1)
	if found == tmp {
		// no decimal. add cents to the number
		tmp += "00"
	}
	val, err := strconv.ParseUint(tmp, 10, 64)
	if err != nil {
		return 0, &tmp, err
	}
	return val, &tmp, nil
}

func ExtractYesNo(s string) *sql.NullBool {
	ss := strings.Fields(strings.ToLower(s))
	for _, w := range ss {
		w = strings.TrimRight(w, " .,;:!?'\"")
		if yes[w] {
			return &sql.NullBool{
				Bool:  true,
				Valid: true,
			}
		}
		if no[w] {
			return &sql.NullBool{
				Bool:  false,
				Valid: true,
			}
		}
	}
	return &sql.NullBool{
		Bool:  false,
		Valid: false,
	}
}

func ExtractAddress(db *sqlx.DB, u *dt.User, s string) (*dt.Address, bool,
	error) {
	addr, err := address.Parse(s)
	if err != nil {
		// check DB for historical information associated with that user
		log.Println("fetching historical address")
		addr, err := u.GetAddress(db, s)
		if err != nil {
			return nil, false, err
		}
		return addr, true, nil
	}
	type addr2S struct {
		XMLName  xml.Name `xml:"Address"`
		ID       string   `xml:"ID,attr"`
		FirmName string
		Address1 string
		Address2 string
		City     string
		State    string
		Zip5     string
		Zip4     string
	}
	addr2Stmp := addr2S{
		ID:       "0",
		Address1: addr.Line2,
		Address2: addr.Line1,
		City:     addr.City,
		State:    addr.State,
		Zip5:     addr.Zip5,
		Zip4:     addr.Zip4,
	}
	if len(addr.Zip) > 0 {
		addr2Stmp.Zip5 = addr.Zip[:5]
	}
	if len(addr.Zip) > 5 {
		addr2Stmp.Zip4 = addr.Zip[5:]
	}
	addrS := struct {
		XMLName    xml.Name `xml:"AddressValidateRequest"`
		USPSUserID string   `xml:"USERID,attr"`
		Address    addr2S
	}{
		USPSUserID: os.Getenv("USPS_USER_ID"),
		Address:    addr2Stmp,
	}
	xmlAddr, err := xml.Marshal(addrS)
	if err != nil {
		return nil, false, err
	}
	log.Println(string(xmlAddr))
	ul := "https://secure.shippingapis.com/ShippingAPI.dll?API=Verify&XML="
	ul += url.QueryEscape(string(xmlAddr))
	response, err := http.Get(ul)
	if err != nil {
		return nil, false, err
	}
	defer response.Body.Close()
	contents, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return nil, false, err
	}
	resp := struct {
		XMLName    xml.Name `xml:"AddressValidateResponse"`
		USPSUserID string   `xml:"USERID,attr"`
		Address    addr2S
	}{
		USPSUserID: os.Getenv("USPS_USER_ID"),
		Address:    addr2Stmp,
	}
	if err = xml.Unmarshal(contents, &resp); err != nil {
		log.Println("USPS response", string(contents))
		return nil, false, err
	}
	a := dt.Address{
		Name:  resp.Address.FirmName,
		Line1: resp.Address.Address2,
		Line2: resp.Address.Address1,
		City:  resp.Address.City,
		State: resp.Address.State,
		Zip5:  resp.Address.Zip5,
		Zip4:  resp.Address.Zip4,
	}
	if len(resp.Address.Zip4) > 0 {
		a.Zip = resp.Address.Zip5 + "-" + resp.Address.Zip4
	} else {
		a.Zip = resp.Address.Zip5
	}
	return &a, false, nil
}
