package app

import "fmt"

func taskActiveLimitError(limit int) *AppError {
	err := NewAppError(400, fmt.Sprintf("同时排队或运行的任务最多 %d 个，请等待已有任务完成", limit))
	err.Reason = ReasonTaskActiveLimitExceeded
	err.Retryable = true
	return err
}
