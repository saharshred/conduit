package normalize

import (
	"fmt"
	"time"

	"github.com/saharshred/conduit/internal/banks"
	"github.com/saharshred/conduit/internal/schema"
)

// Each mock bank's native fields, deliberately different from the others —
// this is the actual normalization work, not busywork: three real bank
// aggregation APIs really do disagree on field names, amount units, and
// timestamp formats like this.

// MapperBankA: {tx_id, acct, amount (dollars, float), merchant, mcc, ts}
func MapperBankA(n banks.NativeTxn) (schema.Transaction, error) {
	amountDollars, _ := n["amount"].(float64)
	ts, err := parseTime(n["ts"])
	if err != nil {
		return schema.Transaction{}, err
	}
	return schema.Transaction{
		Bank:             "bank-a",
		NativeID:         str(n["tx_id"]),
		AccountID:        str(n["acct"]),
		AmountCents:      int64(amountDollars * 100),
		Currency:         "USD",
		MerchantName:     str(n["merchant"]),
		MerchantCategory: str(n["mcc"]),
		Timestamp:        ts,
	}, nil
}

// MapperBankB: {id, account_number, amount_cents (int), desc, category_code, posted_at}
func MapperBankB(n banks.NativeTxn) (schema.Transaction, error) {
	amountCents, _ := n["amount_cents"].(float64) // JSON numbers decode as float64
	ts, err := parseTime(n["posted_at"])
	if err != nil {
		return schema.Transaction{}, err
	}
	return schema.Transaction{
		Bank:             "bank-b",
		NativeID:         str(n["id"]),
		AccountID:        str(n["account_number"]),
		AmountCents:      int64(amountCents),
		Currency:         "USD",
		MerchantName:     str(n["desc"]),
		MerchantCategory: str(n["category_code"]),
		Timestamp:        ts,
	}, nil
}

// MapperBankC: {uuid, account, cents (int), payee, category, when}
func MapperBankC(n banks.NativeTxn) (schema.Transaction, error) {
	cents, _ := n["cents"].(float64)
	ts, err := parseTime(n["when"])
	if err != nil {
		return schema.Transaction{}, err
	}
	return schema.Transaction{
		Bank:             "bank-c",
		NativeID:         str(n["uuid"]),
		AccountID:        str(n["account"]),
		AmountCents:      int64(cents),
		Currency:         "USD",
		MerchantName:     str(n["payee"]),
		MerchantCategory: str(n["category"]),
		Timestamp:        ts,
	}, nil
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func parseTime(v any) (time.Time, error) {
	s, ok := v.(string)
	if !ok {
		return time.Time{}, fmt.Errorf("normalize: timestamp field missing or not a string: %v", v)
	}
	return time.Parse(time.RFC3339, s)
}
