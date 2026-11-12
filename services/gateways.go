package services

import (
	"context"
	"log/slog"
	"strconv"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/i18n"
)

// Sender delivers one-time codes and messages. The default implementation only logs them; plug an SMS,
// WhatsApp or e-mail provider in here.
type Sender interface {
	Send(ctx context.Context, channel, to, message string) error
}

// LogSender writes outgoing messages to the log.
type LogSender struct{}

func (LogSender) Send(ctx context.Context, channel, to, message string) error {
	helper.Log(ctx, slog.LevelInfo, helper.Sprintf("outgoing %s to %s (no provider configured): %s", channel, to, message), "LogSender-Send", "")
	return nil
}

// Payments creates payment links for invoices and top-ups. The default returns manual-transfer instructions;
// plug a gateway (NICEPay, Midtrans, Xendit …) in here.
type Payments interface {
	// Create returns where and how to pay; the instructions are written in lang.
	Create(ctx context.Context, reference string, amount float64, method string, lang i18n.Lang) (url, instructions string, err error)
}

// ManualPayments asks for a transfer that carries the reference.
type ManualPayments struct{}

func (ManualPayments) Create(_ context.Context, reference string, amount float64, method string, lang i18n.Lang) (url, instructions string, err error) {
	return "", lang.T("Pay Rp {amount} via {method} and include reference {reference}. The payment is confirmed once it is received.",
		"amount", strconv.FormatFloat(amount, 'f', 0, 64), "method", i18n.Tr(method), "reference", reference), nil
}

// round2 rounds money to two decimals, half away from zero.
func round2(value float64) float64 {
	if value < 0 {
		return -round2(-value)
	}
	return float64(int64(value*100+0.5)) / 100
}
