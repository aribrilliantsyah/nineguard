package traffic_test

import (
	"testing"

	"nineguard/internal/traffic"
)

func TestCompareTokens(t *testing.T) {
	// 1. Zero or negative tokens return nil
	if res := traffic.CompareTokens(0); res != nil {
		t.Fatalf("expected nil for 0 tokens, got %+v", res)
	}
	if res := traffic.CompareTokens(-100); res != nil {
		t.Fatalf("expected nil for negative tokens, got %+v", res)
	}

	// 2. Below the smallest book (The Little Prince has ~22,666 tokens)
	// 10,000 tokens / 22,666 = ~44%
	resSmall := traffic.CompareTokens(10000)
	if resSmall == nil {
		t.Fatalf("expected non-nil comparison for 10000 tokens")
	}
	if resSmall.Book != "The Little Prince" {
		t.Fatalf("expected 'The Little Prince', got %s", resSmall.Book)
	}
	if resSmall.Multiple >= 1.0 {
		t.Fatalf("expected multiple < 1.0 for small token count, got %v", resSmall.Multiple)
	}
	if resSmall.Text == "" {
		t.Fatalf("expected non-empty text")
	}

	// 3. Exactly equal or larger than Animal Farm (40,000 tokens) but smaller than Harry Potter (102,666 tokens)
	resAnimal := traffic.CompareTokens(50000)
	if resAnimal == nil {
		t.Fatalf("expected non-nil comparison")
	}
	if resAnimal.Book != "Animal Farm" {
		t.Fatalf("expected 'Animal Farm', got %s", resAnimal.Book)
	}
	if resAnimal.Multiple < 1.0 || resAnimal.Multiple > 2.0 {
		t.Fatalf("expected multiple around 1.25, got %v", resAnimal.Multiple)
	}

	// 4. Very large total above English Wikipedia (6,000,000,000 tokens)
	resWiki := traffic.CompareTokens(12000000000)
	if resWiki == nil {
		t.Fatalf("expected non-nil comparison for 12B tokens")
	}
	if resWiki.Book != "English Wikipedia" {
		t.Fatalf("expected 'English Wikipedia', got %s", resWiki.Book)
	}
	if resWiki.Multiple < 1.9 || resWiki.Multiple > 2.1 {
		t.Fatalf("expected multiple around 2.0, got %v", resWiki.Multiple)
	}
}
