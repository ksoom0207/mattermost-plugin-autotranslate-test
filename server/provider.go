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
	Model       string  `json:"model"`
	Prompt      string  `json:"prompt"`
	MaxTokens   int     `json:"max_tokens,omitempty"`
	Temperature float64 `json:"temperature,omitempty"`
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

	// Prepare request
	reqBody := VLLMRequest{
		Model:       p.model,
		Prompt:      prompt,
		MaxTokens:   2048,
		Temperature: 0.3,
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

	// Extract translated text from response
	translatedText := strings.TrimSpace(vllmResp.Choices[0].Text)
	return translatedText, nil
}

// createTranslationPrompt creates a translation prompt for the LLM
func (p *VLLMProvider) createTranslationPrompt(text, sourceLang, targetLang string) string {
	sourceLanguageName := getLanguageName(sourceLang)
	targetLanguageName := getLanguageName(targetLang)

	if sourceLang == "auto" {
		return fmt.Sprintf("Translate the following text to %s. Only provide the translation, nothing else.\n\nText: %s\n\nTranslation:", targetLanguageName, text)
	}

	return fmt.Sprintf("Translate the following text from %s to %s. Only provide the translation, nothing else.\n\nText: %s\n\nTranslation:", sourceLanguageName, targetLanguageName, text)
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
