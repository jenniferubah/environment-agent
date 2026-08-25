package container

import (
	"errors"
	"net/http"

	v1alpha1 "github.com/dcm-project/environment-agent/api/container/v1alpha1"
	"github.com/dcm-project/environment-agent/internal/openshift/container/httperror"
	oapigen "github.com/dcm-project/environment-agent/internal/openshift/container/oapi/server"
	"github.com/dcm-project/environment-agent/internal/openshift/container/store"
	"github.com/dcm-project/environment-agent/internal/openshift/container/util"
)

// ptrOrNil converts a validationError's Pointer to *string for the wire type.
// Returns nil when pointer is empty (error not attributable to a single field).
func ptrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return util.Ptr(s)
}

func newCreateError400(detail, pointer, requestPath string) oapigen.CreateContainer400ApplicationProblemPlusJSONResponse {
	return oapigen.CreateContainer400ApplicationProblemPlusJSONResponse{
		Type:     v1alpha1.INVALIDARGUMENT,
		Title:    httperror.InvalidArgumentTitle,
		Status:   util.Ptr(int32(http.StatusBadRequest)),
		Detail:   &detail,
		Pointer:  ptrOrNil(pointer),
		Instance: &requestPath,
	}
}

func newCreateMultiError400(errs []validationError, requestPath string) oapigen.CreateContainer400ApplicationProblemPlusJSONResponse {
	if len(errs) < 2 {
		panic("newCreateMultiError400 requires at least 2 errors")
	}
	errorDetails := make([]v1alpha1.ErrorDetail, len(errs))
	for i, e := range errs {
		errorDetails[i] = v1alpha1.ErrorDetail{
			Detail:  e.Detail,
			Pointer: ptrOrNil(e.Pointer),
		}
	}
	return oapigen.CreateContainer400ApplicationProblemPlusJSONResponse{
		Type:     v1alpha1.INVALIDARGUMENT,
		Title:    httperror.InvalidArgumentTitle,
		Status:   util.Ptr(int32(http.StatusBadRequest)),
		Detail:   util.Ptr(httperror.InvalidArgumentMultiDetail),
		Instance: &requestPath,
		Errors:   &errorDetails,
	}
}

func (h *Handler) mapCreateError(err error, requestPath string) oapigen.CreateContainerResponseObject {
	var conflict *store.ConflictError
	if errors.As(err, &conflict) {
		return oapigen.CreateContainer409ApplicationProblemPlusJSONResponse{
			Type:     v1alpha1.ALREADYEXISTS,
			Title:    httperror.AlreadyExistsTitle,
			Status:   util.Ptr(int32(http.StatusConflict)),
			Detail:   util.Ptr(err.Error()),
			Instance: &requestPath,
		}
	}

	var invalid *store.InvalidArgumentError
	if errors.As(err, &invalid) {
		return newCreateError400(err.Error(), "", requestPath)
	}

	h.logger.Error("unexpected error in CreateContainer", "error", err)
	return oapigen.CreateContainer500ApplicationProblemPlusJSONResponse{
		Type:     v1alpha1.INTERNAL,
		Title:    httperror.InternalTitle,
		Status:   util.Ptr(int32(http.StatusInternalServerError)),
		Detail:   util.Ptr(httperror.InternalDetail),
		Instance: &requestPath,
	}
}

func (h *Handler) mapGetError(err error, requestPath string) oapigen.GetContainerResponseObject {
	var notFound *store.NotFoundError
	if errors.As(err, &notFound) {
		return oapigen.GetContainer404ApplicationProblemPlusJSONResponse{
			Type:     v1alpha1.NOTFOUND,
			Title:    httperror.NotFoundTitle,
			Status:   util.Ptr(int32(http.StatusNotFound)),
			Detail:   util.Ptr(err.Error()),
			Instance: &requestPath,
		}
	}

	h.logger.Error("unexpected error in GetContainer", "error", err)
	return oapigen.GetContainer500ApplicationProblemPlusJSONResponse{
		Type:     v1alpha1.INTERNAL,
		Title:    httperror.InternalTitle,
		Status:   util.Ptr(int32(http.StatusInternalServerError)),
		Detail:   util.Ptr(httperror.InternalDetail),
		Instance: &requestPath,
	}
}

func (h *Handler) mapDeleteError(err error, requestPath string) oapigen.DeleteContainerResponseObject {
	var notFound *store.NotFoundError
	if errors.As(err, &notFound) {
		return oapigen.DeleteContainer404ApplicationProblemPlusJSONResponse{
			Type:     v1alpha1.NOTFOUND,
			Title:    httperror.NotFoundTitle,
			Status:   util.Ptr(int32(http.StatusNotFound)),
			Detail:   util.Ptr(err.Error()),
			Instance: &requestPath,
		}
	}

	h.logger.Error("unexpected error in DeleteContainer", "error", err)
	return oapigen.DeleteContainer500ApplicationProblemPlusJSONResponse{
		Type:     v1alpha1.INTERNAL,
		Title:    httperror.InternalTitle,
		Status:   util.Ptr(int32(http.StatusInternalServerError)),
		Detail:   util.Ptr(httperror.InternalDetail),
		Instance: &requestPath,
	}
}

func (h *Handler) mapListError(err error, requestPath string) oapigen.ListContainersResponseObject {
	var invalid *store.InvalidArgumentError
	if errors.As(err, &invalid) {
		return oapigen.ListContainers400ApplicationProblemPlusJSONResponse{
			Type:     v1alpha1.INVALIDARGUMENT,
			Title:    httperror.InvalidArgumentTitle,
			Status:   util.Ptr(int32(http.StatusBadRequest)),
			Detail:   util.Ptr(err.Error()),
			Instance: &requestPath,
		}
	}

	h.logger.Error("unexpected error in ListContainers", "error", err)
	return oapigen.ListContainers500ApplicationProblemPlusJSONResponse{
		Type:     v1alpha1.INTERNAL,
		Title:    httperror.InternalTitle,
		Status:   util.Ptr(int32(http.StatusInternalServerError)),
		Detail:   util.Ptr(httperror.InternalDetail),
		Instance: &requestPath,
	}
}
