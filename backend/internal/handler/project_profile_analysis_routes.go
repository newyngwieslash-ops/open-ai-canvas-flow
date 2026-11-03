package handler

import (
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"yingce/backend/internal/service"
)

func profileInvalidRequest() error {
	return &service.AppError{Status: 400, Code: 400, Reason: "asset_profile_analysis_invalid_request", Message: "分析请求格式无效"}
}
func bindProfileRequest(c *gin.Context, max int64, target any) error {
	raw, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, max))
	if err != nil {
		return profileInvalidRequest()
	}
	return service.StrictProfileAnalysisJSON(raw, target)
}
func registerProjectProfileAnalysisRoutes(r *gin.RouterGroup, svc *service.Service) {
	base := "/projects/:id/assets/:assetId/profile-analyses"
	r.POST(base, func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		var req service.CreateProfileAnalysisRequest
		if err := bindProfileRequest(c, 16<<10, &req); err != nil {
			failService(c, err)
			return
		}
		view, err := svc.CreateProjectProfileAnalysis(user.ID, c.Param("id"), c.Param("assetId"), req)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"analysis": view})
	})
	r.GET(base, func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		limit := 20
		if raw, present := c.GetQuery("limit"); present {
			limit, err = strconv.Atoi(raw)
			if err != nil {
				failService(c, profileInvalidRequest())
				return
			}
		}
		list, err := svc.ProjectProfileAnalyses(user.ID, c.Param("id"), c.Param("assetId"), c.Query("beforeTaskId"), limit)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, list)
	})
	r.GET(base+"/:taskId", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		view, err := svc.ProjectProfileAnalysis(user.ID, c.Param("id"), c.Param("assetId"), c.Param("taskId"))
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"analysis": view})
	})
	r.POST(base+"/:taskId/adopt", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		var req service.AdoptProfileAnalysisRequest
		if err := bindProfileRequest(c, 8<<10, &req); err != nil {
			failService(c, err)
			return
		}
		result, err := svc.AdoptProjectProfileAnalysis(user.ID, c.Param("id"), c.Param("assetId"), c.Param("taskId"), req)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, result)
	})
}
