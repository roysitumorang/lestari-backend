package controllers

import (
	"errors"
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/i18n"
	"github.com/roysitumorang/lestari-backend/middlewares"
	"github.com/roysitumorang/lestari-backend/models"
	"github.com/roysitumorang/lestari-backend/repositories"
	"github.com/roysitumorang/lestari-backend/sanitizers"
	"github.com/roysitumorang/lestari-backend/services"
)

type categoryController struct {
	authService     *middlewares.Service
	categoryService services.CategoryService
}

func NewCategoryController(
	authService *middlewares.Service,
	categoryService services.CategoryService,
) *categoryController {
	return &categoryController{
		authService:     authService,
		categoryService: categoryService,
	}
}

// Mount registers the routes. The id routes only match UUIDs, so other paths under /categories fall through.
func (q *categoryController) Mount(r fiber.Router) {
	private := r.Group("", q.authService.Middleware())
	private.Get("", q.V1Index)
	private.Post("", q.V1Create)
	private.Post("/bulk-delete", q.V1BulkDelete)
	private.Get("/:id<guid>", q.V1Show)
	private.Put("/:id<guid>", q.V1Update)
	private.Delete("/:id<guid>", q.V1Delete)
}

// categoryError turns the category errors into responses; anything else goes to the error handler as it is.
func categoryError(err error) error {
	switch {
	case errors.Is(err, models.ErrCategoryNotFound):
		return helper.NotFound("Category")
	case errors.Is(err, models.ErrCategoryConflict):
		return helper.Conflict("A {item} with the same code or name already exists.", "item", i18n.Tr("Category"))
	case errors.Is(err, models.ErrCategoryInUse):
		return helper.Conflict("This {item} is still used by other records.", "item", i18n.Tr("Category"))
	}
	return writeError(err)
}

// V1Index godoc
//
//	@Summary		List categories
//	@Description	Categories of the active branch.
//	@Tags			Categories
//	@Param			q			query		string	false	"search in name"
//	@Param			sort		query		string	false	"sort field"		Enums(name, color, sort_order, product_count, description, created_at, updated_at)
//	@Param			order		query		string	false	"sort direction"	Enums(asc, desc)	default(asc)
//	@Param			page		query		int		false	"page, from 1"		default(1)
//	@Param			per_page	query		int		false	"rows per page"		default(25)	maximum(1000)
//	@Success		200			{object}	helper.Response{data=helper.Pagination{rows=[]models.Category}}
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/categories [get]
func (q *categoryController) V1Index(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "CategoryController-V1Index"
	request := sanitizers.FindCategories(c).
		WithBranchIDs(currentBranchID(c))
	response, total, err := q.categoryService.FindCategories(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindCategories")
		return err
	}
	return helper.List(c, response, helper.NewMeta(helper.Page{Page: int(request.Page), PerPage: int(request.Limit)}, int(total)))
}

// V1Show godoc
//
//	@Summary	Get one category
//	@Tags		Categories
//	@Param		id	path		string	true	"category id (UUID)"
//	@Success	200	{object}	helper.Response{data=models.Category}
//	@Failure	401	{object}	helper.Response{data=helper.ErrorData}
//	@Failure	404	{object}	helper.Response{data=helper.ErrorData}
//	@Router		/categories/{id} [get]
func (q *categoryController) V1Show(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "CategoryController-V1Show"
	categoryID, err := sanitizers.ParamID(c, "id", "Category")
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrParamID")
		return err
	}
	response, err := q.categoryService.FindCategory(
		ctx,
		nil,
		models.NewCategoryFilter().
			WithBranchIDs(currentBranchID(c)).
			WithCategoryIDs(categoryID),
	)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindCategory")
		return categoryError(err)
	}
	return helper.OK(c, response)
}

// V1Create godoc
//
//	@Summary	Create a category
//	@Tags		Categories
//	@Param		request	body		models.CategoryRequest	true	"category"
//	@Success	201		{object}	helper.Response{data=models.Category}
//	@Failure	401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure	409		{object}	helper.Response{data=helper.ErrorData}
//	@Failure	422		{object}	helper.Response{data=helper.ErrorData}
//	@Router		/categories [post]
func (q *categoryController) V1Create(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "CategoryController-V1Create"
	request, err := sanitizers.ValidateCategory(c, true)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrValidateCategory")
		return err
	}
	now := time.Now()
	category := request.Apply(&models.Category{
		BranchID:  currentBranchID(c),
		CreatedAt: now,
		UpdatedAt: now,
	})
	tx, err := repositories.BeginTx(ctx)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrBeginTx")
		return err
	}
	defer rollback(ctx, tx, ctxt)
	response, err := q.categoryService.CreateCategory(ctx, tx, category)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateCategory")
		return categoryError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCommit")
		return err
	}
	return helper.Created(c, response)
}

// V1Update godoc
//
//	@Summary		Update a category
//	@Description	Only the attributes present in the body are changed. When no value differs from the stored one nothing is written and updated_at stays as it was.
//	@Tags			Categories
//	@Param			id		path		string					true	"category id (UUID)"
//	@Param			request	body		models.CategoryRequest	true	"attributes to change"
//	@Success		200		{object}	helper.Response{data=models.Category}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		404		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		409		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/categories/{id} [put]
func (q *categoryController) V1Update(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "CategoryController-V1Update"
	categoryID, err := sanitizers.ParamID(c, "id", "Category")
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrParamID")
		return err
	}
	request, err := sanitizers.ValidateCategory(c, false)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrValidateCategory")
		return err
	}
	category, err := q.categoryService.FindCategory(
		ctx,
		nil,
		models.NewCategoryFilter().
			WithBranchIDs(currentBranchID(c)).
			WithCategoryIDs(categoryID),
	)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindCategory")
		return categoryError(err)
	}
	tx, err := repositories.BeginTx(ctx)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrBeginTx")
		return err
	}
	defer rollback(ctx, tx, ctxt)
	request.Apply(category).UpdatedAt = time.Now()
	response, err := q.categoryService.UpdateCategory(ctx, tx, category)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrUpdateCategory")
		return categoryError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCommit")
		return err
	}
	return helper.OK(c, response)
}

// V1Delete godoc
//
//	@Summary		Delete a category
//	@Description	A category that other records still use cannot be deleted (409).
//	@Tags			Categories
//	@Param			id	path	string	true	"category id (UUID)"
//	@Success		204
//	@Failure		401	{object}	helper.Response{data=helper.ErrorData}
//	@Failure		404	{object}	helper.Response{data=helper.ErrorData}
//	@Failure		409	{object}	helper.Response{data=helper.ErrorData}
//	@Router			/categories/{id} [delete]
func (q *categoryController) V1Delete(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "CategoryController-V1Delete"
	categoryID, err := sanitizers.ParamID(c, "id", "Category")
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrParamID")
		return err
	}
	tx, err := repositories.BeginTx(ctx)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrBeginTx")
		return err
	}
	defer rollback(ctx, tx, ctxt)
	if err = q.categoryService.DeleteCategories(
		ctx,
		tx,
		models.NewCategoryFilter().
			WithBranchIDs(currentBranchID(c)).
			WithCategoryIDs(categoryID),
	); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrDeleteCategories")
		return categoryError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCommit")
		return err
	}
	return helper.NoContent(c)
}

// V1BulkDelete godoc
//
//	@Summary		Delete several categories
//	@Description	All or nothing: an unknown id (404) or a category still in use (409) deletes none.
//	@Tags			Categories
//	@Param			request	body		models.BulkDeleteRequest	true	"ids to delete"
//	@Success		200		{object}	helper.Response{data=models.DeletedResponse}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		404		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		409		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/categories/bulk-delete [post]
func (q *categoryController) V1BulkDelete(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "CategoryController-V1BulkDelete"
	categoryIDs, err := sanitizers.BulkDeleteIDs(c)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrBulkDeleteIDs")
		return err
	}
	tx, err := repositories.BeginTx(ctx)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrBeginTx")
		return err
	}
	defer rollback(ctx, tx, ctxt)
	if err = q.categoryService.DeleteCategories(
		ctx,
		tx,
		models.NewCategoryFilter().
			WithBranchIDs(currentBranchID(c)).
			WithCategoryIDs(categoryIDs...),
	); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrDeleteCategories")
		return categoryError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCommit")
		return err
	}
	return helper.OK(c, models.DeletedResponse{Deleted: len(categoryIDs)})
}
