package applicationdto

import (
	"encoding/base64"
	"strconv"
)

// EncodeCursor и DecodeCursor — непрозрачный курсор списка заявок: номер
// последней заявки страницы. Общий для очереди сотрудника и списка клиента.
func EncodeCursor(after int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(after, 10)))
}

func DecodeCursor(s string) (int64, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(string(raw), 10, 64)
}
