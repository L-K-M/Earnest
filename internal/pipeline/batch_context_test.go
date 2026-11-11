package pipeline

import "testing"

func TestEditionBatchContextClearsInheritedTransport(t *testing.T) {
	batch := &openAIEditionBatch{}
	ctx := WithEditionBatch(t.Context(), batch)
	if EditionBatchFrom(ctx) != batch {
		t.Fatal("batch not installed")
	}
	if EditionBatchFrom(WithEditionBatch(ctx, nil)) != nil {
		t.Fatal("disabled configuration inherited the previous batch transport")
	}
	if EditionBatchFrom(t.Context()) != nil {
		t.Fatal("unconfigured context enabled batching")
	}
}
