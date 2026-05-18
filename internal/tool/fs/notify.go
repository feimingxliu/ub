package fs

import (
	"context"
	"fmt"
)

func notifyChanged(ctx context.Context, notifier ChangeNotifier, abs string) string {
	if notifier == nil {
		return ""
	}
	if err := notifier.DidChangeFile(ctx, abs); err != nil {
		return fmt.Sprintf("\nlsp notify failed: %v", err)
	}
	return ""
}
