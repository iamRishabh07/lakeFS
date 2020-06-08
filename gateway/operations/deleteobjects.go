package operations

import (
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/treeverse/lakefs/db"

	gerrors "github.com/treeverse/lakefs/gateway/errors"
	"github.com/treeverse/lakefs/gateway/path"
	"github.com/treeverse/lakefs/gateway/serde"
	"github.com/treeverse/lakefs/permissions"
)

type DeleteObjects struct{}

func (controller *DeleteObjects) RequiredPermissions(request *http.Request, repoId string) ([]permissions.Permission, error) {
	req := &serde.Delete{}
	body, err := ioutil.ReadAll(request.Body)
	if err != nil {
		return nil, err
	}
	_ = request.Body.Close()
	err = DecodeXMLBody(bytes.NewReader(body), req)
	if err != nil {
		return nil, err
	}
	request.Body = ioutil.NopCloser(bytes.NewReader(body))
	perms := make([]permissions.Permission, len(req.Object))
	for i, object := range req.Object {
		perms[i] = permissions.Permission{
			Action:   permissions.DeleteObjectAction,
			Resource: permissions.ObjectArn(repoId, object.Key),
		}
	}

	return perms, nil
}

func (controller *DeleteObjects) Handle(o *RepoOperation) {
	o.Incr("delete_objects")
	req := &serde.Delete{}
	err := DecodeXMLBody(o.Request.Body, req)
	if err != nil {
		o.EncodeError(gerrors.Codes.ToAPIErr(gerrors.ErrBadRequest))
	}
	// delete all the files and collect responses
	errs := make([]serde.DeleteError, 0)
	responses := make([]serde.Deleted, 0)
	for _, obj := range req.Object {
		resolvedPath, err := path.ResolvePath(obj.Key)
		if err != nil {
			errs = append(errs, serde.DeleteError{
				Code:    "ErrDeletingKey",
				Key:     obj.Key,
				Message: fmt.Sprintf("error deleting object: %s", err),
			})
			continue
		}
		lg := o.Log().WithField("key", obj.Key)
		err = o.Index.DeleteObject(o.Repo.Id, resolvedPath.Ref, resolvedPath.Path)
		if err != nil && !errors.Is(err, db.ErrNotFound) {
			lg.WithError(err).Error("failed deleting object")
			errs = append(errs, serde.DeleteError{
				Code:    "ErrDeletingKey",
				Key:     obj.Key,
				Message: fmt.Sprintf("error deleting object: %s", err),
			})
			continue
		} else if errors.Is(err, db.ErrNotFound) {
			lg.Debug("tried to delete a non-existent object")
		} else if err == nil {
			lg.Debug("object set for deletion")
		}
		if !req.Quiet {
			responses = append(responses, serde.Deleted{Key: obj.Key})
		}
	}
	// construct response
	resp := serde.DeleteResult{}
	if len(errs) > 0 {
		resp.Error = errs
	}
	if !req.Quiet && len(responses) > 0 {
		resp.Deleted = responses
	}
	o.EncodeResponse(resp, http.StatusOK)

}
