package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

var ollamaEndpoint = "http://127.0.0.1:11434" // overridden only in tests

var ollamaClient = &http.Client{Timeout: 4 * time.Second}

func ollamaModels(ctx context.Context, path string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ollamaEndpoint+path, nil)
	if err != nil {
		return nil, err
	}
	resp, err := ollamaClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Ollama HTTP %d", resp.StatusCode)
	}
	var result struct {
		Models []struct {
			Name  string `json:"name"`
			Model string `json:"model"`
		} `json:"models"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&result); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(result.Models))
	for _, entry := range result.Models {
		name := entry.Name
		if name == "" {
			name = entry.Model
		}
		names = append(names, name)
	}
	return names, nil
}
func containsOllamaModel(names []string, target string) bool {
	for _, name := range names {
		if strings.EqualFold(name, target) {
			return true
		}
		if !strings.Contains(target, ":") && strings.EqualFold(name, target+":latest") {
			return true
		}
	}
	return false
}
func effectiveLocalModel(c Config) string {
	model := strings.TrimSpace(c.AIModel)
	if model == "" || strings.HasPrefix(model, "gpt-") || model == "qwen2.5:7b" {
		return "qwen3:14b"
	}
	return model
}
func (a *app) localAIStatus(ctx context.Context) map[string]any {
	a.mu.Lock()
	cfg := a.state.Config
	busy := a.aiBusy
	a.mu.Unlock()
	model := effectiveLocalModel(cfg)
	result := map[string]any{"enabled": cfg.LocalAIEnabled && cfg.AIProvider == "ollama", "connected": false, "model": model, "model_available": false, "loaded": false, "busy": busy}
	models, err := ollamaModels(ctx, "/api/tags")
	if err != nil {
		result["error"] = "Ollama не отвечает по localhost:11434"
		return result
	}
	result["connected"] = true
	result["model_available"] = containsOllamaModel(models, model)
	loaded, err := ollamaModels(ctx, "/api/ps")
	if err == nil {
		result["loaded"] = containsOllamaModel(loaded, model)
	}
	return result
}

// Switching ON authorizes local inference but does NOT preload the model into VRAM.
// Switching OFF disallows all new local AI requests and asks Ollama to unload this model.
func (a *app) setLocalAI(ctx context.Context, enabled bool) (string, error) {
	a.mu.Lock()
	if a.aiBusy {
		a.mu.Unlock()
		return "", errors.New("ИИ занят созданием поста. Дождись его завершения")
	}
	cfg := a.state.Config
	a.mu.Unlock()
	oldModel := effectiveLocalModel(cfg)
	if enabled {
		model := effectiveLocalModel(cfg)
		models, err := ensureOllamaDaemon(ctx)
		if err != nil {
			return "", errors.New("Ollama не запущена. Установи или открой Ollama из меню «Пуск», затем повтори включение")
		}
		if !containsOllamaModel(models, model) {
			return "", fmt.Errorf("Модель %s пока не установлена. В PowerShell выполни: ollama pull %s", model, model)
		}
		cfg.AIProvider = "ollama"
		cfg.AIModel = model
		cfg.LocalAIEnabled = true
		// Enabling the switch alone must not start any background inference.
		// Automatic rewriting can be explicitly re-enabled later in settings.
		cfg.AutoRewrite = false
	} else {
		cfg.LocalAIEnabled = false
		if cfg.AIProvider == "ollama" {
			cfg.AutoRewrite = false
		}
	}
	a.mu.Lock()
	if a.aiBusy {
		a.mu.Unlock()
		return "", errors.New("ИИ начал создание поста. Дождись его завершения")
	}
	a.state.Config = cfg
	err := a.persistLocked()
	a.mu.Unlock()
	if err != nil {
		return "", err
	}
	if enabled {
		return "Локальный ИИ включён. Модель будет загружаться только при создании поста", nil
	}
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	if err := unloadOllamaModel(cleanupCtx, oldModel); err != nil {
		return "ИИ выключен. Выгрузку видеопамяти не удалось подтвердить: " + err.Error(), nil
	}
	return "ИИ выключен, модель выгружена (если была загружена). Мониторинг продолжается", nil
}

func unloadOllamaModel(ctx context.Context, model string) error {
	if model == "" {
		return nil
	}
	loaded, err := ollamaModels(ctx, "/api/ps")
	if err != nil {
		return err
	}
	if !containsOllamaModel(loaded, model) {
		return nil
	}
	body, _ := json.Marshal(map[string]any{"model": model, "stream": false, "keep_alive": 0})
	req, err := http.NewRequestWithContext(ctx, "POST", ollamaEndpoint+"/api/generate", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := ollamaClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("Ollama HTTP %d", resp.StatusCode)
	}
	return nil
}

// Bring back the local Ollama service after restarting News Agent. This does
// not start inference, download models, write startup registry keys, or
// change the user's opt-in setting. It does nothing when local AI is off.
func (a *app) startLocalAIWithSavedConsent() {
	a.mu.Lock()
	enabled := a.state.Config.AIProvider == "ollama" && a.state.Config.LocalAIEnabled
	a.mu.Unlock()
	if !enabled {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 14*time.Second)
	defer cancel()
	_, _ = ensureOllamaDaemon(ctx)
}

// The actual language model stays unloaded until a user requests a post.
// This is also used to recover if Ollama was shut down while News Agent runs.
func ensureOllamaDaemon(ctx context.Context) ([]string, error) {
	models, err := ollamaModels(ctx, "/api/tags")
	if err == nil {
		return models, nil
	}
	if !tryStartOllama() {
		return nil, errors.New("Не удалось запустить Ollama. Проверь установку через меню Пуск")
	}
	for i := 0; i < 12; i++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(650 * time.Millisecond):
		}
		models, err = ollamaModels(ctx, "/api/tags")
		if err == nil {
			return models, nil
		}
	}
	return nil, fmt.Errorf("Ollama не ответила после запуска: %w", err)
}
