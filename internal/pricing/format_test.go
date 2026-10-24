package pricing

import (
	"testing"
)

func TestFormatCost(t *testing.T) {
	// 1. USD formatting with comma and cents
	s := FormatCost(1234.56, "USD", 1.0, 0)
	if s != "$1,234.56" {
		t.Fatalf("expected $1,234.56, got %q", s)
	}

	// 2. IDR formatting with dot thousands and zero decimals
	s = FormatCost(100.0, "IDR", 18031.0, 0)
	if s != "Rp 1.803.100" {
		t.Fatalf("expected 'Rp 1.803.100', got %q", s)
	}

	// 3. JPY formatting with zero decimals
	s = FormatCost(10.0, "JPY", 150.0, 0)
	if s != "¥1,500" {
		t.Fatalf("expected '¥1,500', got %q", s)
	}

	// 4. EUR formatting
	s = FormatCost(50.25, "EUR", 0.92, 0)
	if s != "€46.23" {
		t.Fatalf("expected '€46.23', got %q", s)
	}

	// 5. SGD formatting
	s = FormatCost(10.0, "SGD", 1.35, 0)
	if s != "S$13.50" {
		t.Fatalf("expected 'S$13.50', got %q", s)
	}

	// 6. Generic currency code
	s = FormatCost(10.0, "CAD", 1.4, 0)
	if s != "CAD 14.00" {
		t.Fatalf("expected 'CAD 14.00', got %q", s)
	}

	// 7. Sub-unit thresholds: positive amount < 0.01 USD
	s = FormatCost(0.005, "USD", 1.0, 0)
	if s != "<$0.01" {
		t.Fatalf("expected '<$0.01', got %q", s)
	}

	s = FormatCost(0.00001, "IDR", 18000.0, 0) // = 0.18 IDR < 1
	if s != "<Rp 1" {
		t.Fatalf("expected '<Rp 1', got %q", s)
	}

	// 8. Zero cost without unpriced
	s = FormatCost(0, "USD", 1.0, 0)
	if s != "$0.00" {
		t.Fatalf("expected '$0.00', got %q", s)
	}

	s = FormatCost(0, "IDR", 18000.0, 0)
	if s != "Rp 0" {
		t.Fatalf("expected 'Rp 0', got %q", s)
	}

	// 9. Unpriced requests prefix: '≥ '
	s = FormatCost(12.34, "USD", 1.0, 5)
	if s != "≥ $12.34" {
		t.Fatalf("expected '≥ $12.34', got %q", s)
	}

	s = FormatCost(0, "USD", 1.0, 3)
	if s != "≥ $0.00" {
		t.Fatalf("expected '≥ $0.00', got %q", s)
	}
}
