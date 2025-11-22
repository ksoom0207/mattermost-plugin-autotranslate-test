package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/translate"
)

// TranslationProvider defines the interface for translation services
type TranslationProvider interface {
	Translate(text, sourceLang, targetLang string) (string, error)
	GetName() string
}

// AWSTranslateProvider implements TranslationProvider for AWS Translate
type AWSTranslateProvider struct {
	accessKeyID     string
	secretAccessKey string
	region          string
}

// NewAWSTranslateProvider creates a new AWS Translate provider
func NewAWSTranslateProvider(accessKeyID, secretAccessKey, region string) *AWSTranslateProvider {
	return &AWSTranslateProvider{
		accessKeyID:     accessKeyID,
		secretAccessKey: secretAccessKey,
		region:          region,
	}
}

// GetName returns the provider name
func (p *AWSTranslateProvider) GetName() string {
	return "aws"
}

// Translate translates text using AWS Translate
func (p *AWSTranslateProvider) Translate(text, sourceLang, targetLang string) (string, error) {
	sess := session.Must(session.NewSession())
	creds := credentials.NewStaticCredentials(p.accessKeyID, p.secretAccessKey, "")
	_, err := creds.Get()
	if err != nil {
		return "", fmt.Errorf("invalid AWS credentials: %w", err)
	}

	svc := translate.New(sess, aws.NewConfig().WithCredentials(creds).WithRegion(p.region))

	input := translate.TextInput{
		SourceLanguageCode: &sourceLang,
		TargetLanguageCode: &targetLang,
		Text:               &text,
	}

	output, err := svc.Text(&input)
	if err != nil {
		return "", fmt.Errorf("AWS translation failed: %w", err)
	}

	return *output.TranslatedText, nil
}

// VLLMProvider implements TranslationProvider for vLLM API
type VLLMProvider struct {
	apiURL string
	apiKey string
	model  string
}

// NewVLLMProvider creates a new vLLM provider
func NewVLLMProvider(apiURL, apiKey, model string) *VLLMProvider {
	return &VLLMProvider{
		apiURL: apiURL,
		apiKey: apiKey,
		model:  model,
	}
}

// GetName returns the provider name
func (p *VLLMProvider) GetName() string {
	return "vllm"
}

// VLLMRequest represents the request body for vLLM API
type VLLMRequest struct {
	Model       string   `json:"model"`
	Prompt      string   `json:"prompt"`
	MaxTokens   int      `json:"max_tokens,omitempty"`
	Temperature float64  `json:"temperature,omitempty"`
	Stop        []string `json:"stop,omitempty"`
}

// VLLMResponse represents the response from vLLM API
type VLLMResponse struct {
	Choices []struct {
		Text string `json:"text"`
	} `json:"choices"`
}

// Translate translates text using vLLM API
func (p *VLLMProvider) Translate(text, sourceLang, targetLang string) (string, error) {
	// Create translation prompt
	prompt := p.createTranslationPrompt(text, sourceLang, targetLang)

	// Prepare request with optimized parameters for translation
	reqBody := VLLMRequest{
		Model:       p.model,
		Prompt:      prompt,
		MaxTokens:   512, // Reduced from 2048 to prevent long responses
		Temperature: 0.1, // Low temperature for more deterministic translation
		Stop: []string{
			"\n\n",           // Stop at double newline
			"\nNote:",        // Stop at explanation attempts
			"\nExplanation:", // Stop at explanation attempts
			"\nTranslation:", // Stop if it tries to label
			"\n\nInput:",     // Stop if it continues pattern
			"[/INST]",        // Stop at chat template end
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	// Make HTTP request
	req, err := http.NewRequest("POST", p.apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("vLLM API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("vLLM API error (status %d): %s", resp.StatusCode, string(body))
	}

	// Parse response
	var vllmResp VLLMResponse
	if err := json.NewDecoder(resp.Body).Decode(&vllmResp); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	if len(vllmResp.Choices) == 0 {
		return "", fmt.Errorf("no translation returned from vLLM")
	}

	// Extract and clean translated text from response
	translatedText := vllmResp.Choices[0].Text

	// Clean up common unwanted patterns
	translatedText = cleanTranslationOutput(translatedText)

	return translatedText, nil
}

// cleanTranslationOutput removes common unwanted patterns from LLM output
func cleanTranslationOutput(text string) string {
	// Trim whitespace
	text = strings.TrimSpace(text)

	// Remove common prefixes that models add
	unwantedPrefixes := []string{
		"Translation: ",
		"Translated text: ",
		"Here is the translation: ",
		"The translation is: ",
		"Output: ",
		"Answer: ",
		"Result: ",
	}

	for _, prefix := range unwantedPrefixes {
		if strings.HasPrefix(text, prefix) {
			text = strings.TrimPrefix(text, prefix)
			text = strings.TrimSpace(text)
		}
	}

	// Remove quotes if entire text is quoted
	if (strings.HasPrefix(text, "\"") && strings.HasSuffix(text, "\"")) ||
		(strings.HasPrefix(text, "'") && strings.HasSuffix(text, "'")) {
		text = text[1 : len(text)-1]
		text = strings.TrimSpace(text)
	}

	// Remove trailing notes or explanations
	if idx := strings.Index(text, "\n\nNote:"); idx != -1 {
		text = text[:idx]
	}
	if idx := strings.Index(text, "\n\nExplanation:"); idx != -1 {
		text = text[:idx]
	}

	return strings.TrimSpace(text)
}

// createTranslationPrompt creates a translation prompt for the LLM
func (p *VLLMProvider) createTranslationPrompt(text, sourceLang, targetLang string) string {
	sourceLanguageName := getLanguageName(sourceLang)
	targetLanguageName := getLanguageName(targetLang)

	// Add language-specific clarifications to prevent confusion
	targetClarification := getLanguageClarification(targetLang)
	sourceClarification := ""
	if sourceLang != "auto" {
		sourceClarification = getLanguageClarification(sourceLang)
	}

	if sourceLang == "auto" {
		// Simplified prompt for auto-detect mode
		return fmt.Sprintf(`Translate to %s%s. Reply with ONLY the translation.

%s`, targetLanguageName, targetClarification, text)
	}

	// Ultra-concise prompt to minimize extra output
	return fmt.Sprintf(`Translate from %s%s to %s%s. Reply with ONLY the translation.

%s`, sourceLanguageName, sourceClarification, targetLanguageName, targetClarification, text)
}

// LiteLLMProvider implements TranslationProvider for LiteLLM (OpenAI-compatible API)
type LiteLLMProvider struct {
	apiURL string
	apiKey string
	model  string
}

// NewLiteLLMProvider creates a new LiteLLM provider
func NewLiteLLMProvider(apiURL, apiKey, model string) *LiteLLMProvider {
	return &LiteLLMProvider{
		apiURL: apiURL,
		apiKey: apiKey,
		model:  model,
	}
}

// GetName returns the provider name
func (p *LiteLLMProvider) GetName() string {
	return "litellm"
}

// LiteLLMChatRequest represents the chat completion request for LiteLLM
type LiteLLMChatRequest struct {
	Model       string               `json:"model"`
	Messages    []LiteLLMChatMessage `json:"messages"`
	Temperature float64              `json:"temperature,omitempty"`
	MaxTokens   int                  `json:"max_tokens,omitempty"`
}

// LiteLLMChatMessage represents a chat message
type LiteLLMChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// LiteLLMChatResponse represents the response from LiteLLM
type LiteLLMChatResponse struct {
	Choices []struct {
		Message LiteLLMChatMessage `json:"message"`
	} `json:"choices"`
}

// Translate translates text using LiteLLM API
func (p *LiteLLMProvider) Translate(text, sourceLang, targetLang string) (string, error) {
	// Create translation prompt
	sourceLanguageName := getLanguageName(sourceLang)
	targetLanguageName := getLanguageName(targetLang)

	// Add language-specific clarifications to prevent confusion
	targetClarification := getLanguageClarification(targetLang)
	sourceClarification := ""
	if sourceLang != "auto" {
		sourceClarification = getLanguageClarification(sourceLang)
	}

	var userPrompt string
	if sourceLang == "auto" {
		userPrompt = fmt.Sprintf("Translate to %s%s:\n\n%s", targetLanguageName, targetClarification, text)
	} else {
		userPrompt = fmt.Sprintf("Translate from %s%s to %s%s:\n\n%s", sourceLanguageName, sourceClarification, targetLanguageName, targetClarification, text)
	}

	// Prepare request with optimized parameters
	// Higher limits for local LiteLLM deployment
	reqBody := LiteLLMChatRequest{
		Model: p.model,
		Messages: []LiteLLMChatMessage{
			{
				Role:    "system",
				Content: "You are a translation system. Output ONLY the translated text without any explanations, notes, or additional commentary.",
			},
			{
				Role:    "user",
				Content: userPrompt,
			},
		},
		Temperature: 0.3,  // Slightly higher for more natural translations
		MaxTokens:   2048, // Higher limit for longer texts
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	// Make HTTP request
	req, err := http.NewRequest("POST", p.apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("LiteLLM API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("LiteLLM API error (status %d): %s", resp.StatusCode, string(body))
	}

	// Parse response
	var litellmResp LiteLLMChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&litellmResp); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	if len(litellmResp.Choices) == 0 {
		return "", fmt.Errorf("no translation returned from LiteLLM")
	}

	// Extract and clean translated text from response
	translatedText := litellmResp.Choices[0].Message.Content

	// Clean up common unwanted patterns (reuse cleaning function)
	translatedText = cleanTranslationOutput(translatedText)

	return translatedText, nil
}

// getLanguageClarification provides additional context for commonly confused languages
func getLanguageClarification(code string) string {
	clarifications := map[string]string{
		"ko":    " (한국어, using Hangul script, NOT Chinese)",
		"ja":    " (日本語, using Hiragana/Katakana/Kanji, NOT Chinese or Korean)",
		"zh":    " (中文简体, Simplified Chinese characters)",
		"zh-TW": " (中文繁體, Traditional Chinese characters)",
		"en":    " (English)",
		"ar":    " (العربية, Arabic script)",
		"he":    " (עברית, Hebrew script)",
		"hi":    " (हिन्दी, Devanagari script)",
		"ru":    " (Русский, Cyrillic script)",
		"th":    " (ไทย, Thai script)",
	}

	if clarification, ok := clarifications[code]; ok {
		return clarification
	}
	return ""
}

// getLanguageName converts language code to full language name
func getLanguageName(code string) string {
	languageNames := map[string]string{
		"auto":  "Auto-detect",
		"af":    "Afrikaans",
		"sq":    "Albanian",
		"am":    "Amharic",
		"ar":    "Arabic",
		"hy":    "Armenian",
		"az":    "Azerbaijani",
		"bn":    "Bengali",
		"bs":    "Bosnian",
		"bg":    "Bulgarian",
		"ca":    "Catalan",
		"zh":    "Chinese (Simplified)",
		"zh-TW": "Chinese (Traditional)",
		"hr":    "Croatian",
		"cs":    "Czech",
		"da":    "Danish",
		"fa-AF": "Dari",
		"nl":    "Dutch",
		"en":    "English",
		"et":    "Estonian",
		"fa":    "Farsi (Persian)",
		"tl":    "Filipino, Tagalog",
		"fi":    "Finnish",
		"fr":    "French",
		"fr-CA": "French (Canada)",
		"ka":    "Georgian",
		"de":    "German",
		"el":    "Greek",
		"gu":    "Gujarati",
		"ht":    "Haitian Creole",
		"ha":    "Hausa",
		"he":    "Hebrew",
		"hi":    "Hindi",
		"hu":    "Hungarian",
		"is":    "Icelandic",
		"id":    "Indonesian",
		"it":    "Italian",
		"ja":    "Japanese",
		"kn":    "Kannada",
		"kk":    "Kazakh",
		"ko":    "Korean",
		"lv":    "Latvian",
		"lt":    "Lithuanian",
		"mk":    "Macedonian",
		"ms":    "Malay",
		"ml":    "Malayalam",
		"mt":    "Maltese",
		"mr":    "Marathi",
		"mn":    "Mongolian",
		"no":    "Norwegian",
		"ps":    "Pashto",
		"pl":    "Polish",
		"pt":    "Portuguese",
		"pa":    "Punjabi",
		"ro":    "Romanian",
		"ru":    "Russian",
		"sr":    "Serbian",
		"si":    "Sinhala",
		"sk":    "Slovak",
		"sl":    "Slovenian",
		"so":    "Somali",
		"es":    "Spanish",
		"es-MX": "Spanish (Mexico)",
		"sw":    "Swahili",
		"sv":    "Swedish",
		"ta":    "Tamil",
		"te":    "Telugu",
		"th":    "Thai",
		"tr":    "Turkish",
		"uk":    "Ukrainian",
		"ur":    "Urdu",
		"uz":    "Uzbek",
		"vi":    "Vietnamese",
		"cy":    "Welsh",
	}

	if name, ok := languageNames[code]; ok {
		return name
	}
	return code
}
