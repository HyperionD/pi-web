package piweb

import (
	"context"
	"encoding/json"
	"errors"
)

func runtimeModelInitializer(settings []ModelStartupSettings) RuntimeInitializer {
	if len(settings) == 0 || settings[0].Provider == "" || settings[0].Model == "" {
		return nil
	}
	selected := settings[0]
	return func(ctx context.Context, runtime PiRuntime) error {
		return initializeRuntimeModel(ctx, runtime, selected)
	}
}

func initializeRuntimeModel(ctx context.Context, runtime PiRuntime, settings ModelStartupSettings) error {
	if settings.Provider != "" && settings.Model != "" {
		if err := sendRuntimeModelCommand(ctx, runtime, RPCCommand{
			Type:    "set_model",
			Payload: mustJSON(map[string]string{"provider": settings.Provider, "modelId": settings.Model}),
		}); err != nil {
			return err
		}
	}
	if settings.ThinkingLevel == "" {
		return nil
	}
	return sendRuntimeModelCommand(ctx, runtime, RPCCommand{
		Type:    "set_thinking_level",
		Payload: mustJSON(map[string]string{"level": settings.ThinkingLevel}),
	})
}

func sendRuntimeModelCommand(ctx context.Context, runtime PiRuntime, command RPCCommand) error {
	response, err := runtime.Send(ctx, command)
	if err != nil {
		return err
	}
	if !response.Success {
		if response.Error == "" {
			response.Error = "pi model configuration failed"
		}
		return errors.New(response.Error)
	}
	return nil
}

func mustJSON(value any) json.RawMessage {
	encoded, _ := json.Marshal(value)
	return encoded
}
