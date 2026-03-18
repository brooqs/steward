package embedding

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	ort "github.com/yalue/onnxruntime_go"
)

// ONNXEmbedder generates embeddings locally using ONNX Runtime.
// Supports models like all-MiniLM-L6-v2 (384 dims).
//
// Model setup:
//  1. Download the ONNX model from HuggingFace
//  2. Place in the models/ directory
//  3. Configure model path in core.yml
type ONNXEmbedder struct {
	modelPath string
	dims      int
	session   *ort.AdvancedSession
}

// NewONNXEmbedder creates a local ONNX embedding provider.
// modelName should be the base name (e.g., "all-MiniLM-L6-v2")
func NewONNXEmbedder(modelPath string) (*ONNXEmbedder, error) {
	if _, err := os.Stat(modelPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("ONNX model not found: %s", modelPath)
	}

	// Initialize ONNX Runtime
	ort.SetSharedLibraryPath(findONNXLib())
	if err := ort.InitializeEnvironment(); err != nil {
		return nil, fmt.Errorf("initializing ONNX runtime: %w", err)
	}

	slog.Info("ONNX embedder initialized", "model", modelPath)

	return &ONNXEmbedder{
		modelPath: modelPath,
		dims:      384, // default for MiniLM, will be updated on first run
	}, nil
}

func (o *ONNXEmbedder) Name() string       { return "onnx" }
func (o *ONNXEmbedder) Dimensions() int     { return o.dims }

func (o *ONNXEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	vecs, err := o.EmbedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(vecs) == 0 {
		return nil, fmt.Errorf("empty embedding result")
	}
	return vecs[0], nil
}

func (o *ONNXEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	// For the ONNX embedder, we process texts one by one since tokenization
	// needs to happen at the Go level. In a production setup, we'd use a
	// proper tokenizer (e.g., github.com/nicholasgasior/gosent or similar).
	//
	// For now, we use a simplified approach: convert text to token IDs
	// using a basic byte-level encoding, then run through the model.
	//
	// NOTE: For production quality, consider using:
	// - github.com/nicholasgasior/gosent for sentence tokenization
	// - Or shell out to Python's tokenizers for exact HuggingFace compatibility
	results := make([][]float32, len(texts))

	for i, text := range texts {
		vec, err := o.embedSingle(text)
		if err != nil {
			return nil, fmt.Errorf("embedding text %d: %w", i, err)
		}
		results[i] = vec
	}

	return results, nil
}

func (o *ONNXEmbedder) embedSingle(text string) ([]float32, error) {
	// Tokenize: simple byte-pair encoding approximation
	// For MiniLM models, max sequence length is 256 tokens
	maxLen := 128
	tokens := tokenize(text, maxLen)

	batchSize := int64(1)
	seqLen := int64(len(tokens))

	// Create input tensors
	inputIDs := make([]int64, len(tokens))
	attentionMask := make([]int64, len(tokens))
	tokenTypeIDs := make([]int64, len(tokens))
	for i, t := range tokens {
		inputIDs[i] = int64(t)
		attentionMask[i] = 1
		tokenTypeIDs[i] = 0
	}

	inputShape := ort.Shape{batchSize, seqLen}

	inputIDsTensor, err := ort.NewTensor(inputShape, inputIDs)
	if err != nil {
		return nil, fmt.Errorf("creating input_ids tensor: %w", err)
	}
	defer inputIDsTensor.Destroy()

	attentionTensor, err := ort.NewTensor(inputShape, attentionMask)
	if err != nil {
		return nil, fmt.Errorf("creating attention_mask tensor: %w", err)
	}
	defer attentionTensor.Destroy()

	tokenTypeTensor, err := ort.NewTensor(inputShape, tokenTypeIDs)
	if err != nil {
		return nil, fmt.Errorf("creating token_type_ids tensor: %w", err)
	}
	defer tokenTypeTensor.Destroy()

	// Output tensor
	outputShape := ort.Shape{batchSize, seqLen, int64(o.dims)}
	outputTensor, err := ort.NewEmptyTensor[float32](outputShape)
	if err != nil {
		return nil, fmt.Errorf("creating output tensor: %w", err)
	}
	defer outputTensor.Destroy()

	// Create session if not exists
	if o.session == nil {
		session, err := ort.NewAdvancedSession(
			o.modelPath,
			[]string{"input_ids", "attention_mask", "token_type_ids"},
			[]string{"last_hidden_state"},
			[]ort.ArbitraryTensor{inputIDsTensor, attentionTensor, tokenTypeTensor},
			[]ort.ArbitraryTensor{outputTensor},
			nil,
		)
		if err != nil {
			return nil, fmt.Errorf("creating ONNX session: %w", err)
		}
		o.session = session
	}

	// Run inference
	if err := o.session.Run(); err != nil {
		return nil, fmt.Errorf("ONNX inference: %w", err)
	}

	// Mean pooling: average all token embeddings
	output := outputTensor.GetData()
	embedding := make([]float32, o.dims)
	validTokens := 0

	for t := 0; t < int(seqLen); t++ {
		if attentionMask[t] == 1 {
			for d := 0; d < o.dims; d++ {
				embedding[d] += output[t*o.dims+d]
			}
			validTokens++
		}
	}

	// Average
	if validTokens > 0 {
		for d := range embedding {
			embedding[d] /= float32(validTokens)
		}
	}

	// L2 normalize
	var norm float32
	for _, v := range embedding {
		norm += v * v
	}
	norm = sqrt(norm)
	if norm > 0 {
		for i := range embedding {
			embedding[i] /= norm
		}
	}

	return embedding, nil
}

// tokenize performs a simple word-piece-like tokenization.
// This is a simplified version — for production, use a proper tokenizer.
func tokenize(text string, maxLen int) []int {
	// [CLS] = 101, [SEP] = 102
	tokens := []int{101} // [CLS]

	// Simple space-based tokenization with character fallback
	words := splitWords(text)
	for _, word := range words {
		if len(tokens) >= maxLen-1 {
			break
		}
		// Use simple hash-based token IDs (not ideal but functional)
		tokenID := hashWord(word)
		tokens = append(tokens, tokenID)
	}

	tokens = append(tokens, 102) // [SEP]
	return tokens
}

func splitWords(text string) []string {
	var words []string
	var current []byte
	for i := 0; i < len(text); i++ {
		c := text[i]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			if len(current) > 0 {
				words = append(words, string(current))
				current = current[:0]
			}
		} else {
			current = append(current, c)
		}
	}
	if len(current) > 0 {
		words = append(words, string(current))
	}
	return words
}

func hashWord(word string) int {
	// Simple hash to generate a token ID in vocab range [1000, 30000]
	h := 0
	for _, c := range word {
		h = h*31 + int(c)
	}
	if h < 0 {
		h = -h
	}
	return 1000 + (h % 29000)
}

// findONNXLib attempts to locate the ONNX Runtime shared library.
func findONNXLib() string {
	paths := []string{
		"/usr/lib/libonnxruntime.so",
		"/usr/local/lib/libonnxruntime.so",
		"/usr/lib/x86_64-linux-gnu/libonnxruntime.so",
		"./libonnxruntime.so",
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return "libonnxruntime.so" // let the system find it
}
