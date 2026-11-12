package controllers

import (
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/roysitumorang/lestari-backend/config"
	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/i18n"
	"github.com/roysitumorang/lestari-backend/middlewares"
	"github.com/roysitumorang/lestari-backend/models"
	"github.com/roysitumorang/lestari-backend/sanitizers"
	"github.com/roysitumorang/lestari-backend/services"
)

// maxUploadSize is the largest file an upload accepts.
const maxUploadSize = 10 << 20

// allowedUploads maps the accepted content types to the extension a stored file gets.
var allowedUploads = map[string]string{
	"image/jpeg":      ".jpg",
	"image/png":       ".png",
	"image/webp":      ".webp",
	"image/gif":       ".gif",
	"image/svg+xml":   ".svg",
	"application/pdf": ".pdf",
}

// Uploader saves multipart files to the upload directory; controllers that accept files share one.
type Uploader struct {
	config        config.Config
	uploadService services.UploadService
}

func NewUploader(
	cfg config.Config,
	uploadService services.UploadService,
) *Uploader {
	return &Uploader{
		config:        cfg,
		uploadService: uploadService,
	}
}

// Save stores the file of a form field (an image or a PDF of at most 10 MB) and returns its public URL.
func (q *Uploader) Save(c fiber.Ctx, field, purpose string) (*models.Upload, error) {
	ctx := c.Context()
	ctxt := "Uploader-Save"
	header, err := c.FormFile(field)
	if err != nil {
		return nil, helper.Invalid(field, "Choose a file to upload.")
	}
	if header.Size > maxUploadSize {
		return nil, helper.Invalid(field, "The file is larger than 10 MB.")
	}
	file, err := header.Open()
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrOpen")
		return nil, err
	}
	defer file.Close()
	// the type comes from the content, not from the name the client sent
	head := make([]byte, 512)
	n, _ := file.Read(head)
	mimeType := http.DetectContentType(head[:n])
	if strings.HasSuffix(strings.ToLower(header.Filename), ".svg") && strings.Contains(string(head[:n]), "<svg") {
		mimeType = "image/svg+xml"
	}
	extension, ok := allowedUploads[mimeType]
	if !ok {
		return nil, helper.Invalid(field, "Only images (JPG, PNG, WebP, GIF, SVG) and PDF files are allowed.")
	}
	storeID := currentStoreID(c)
	path := filepath.Join(storeID.String(), middlewares.RandomToken(16)+extension)
	destination := filepath.Join(q.config.UploadDir, path)
	if err = os.MkdirAll(filepath.Dir(destination), 0o750); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrMkdirAll")
		return nil, err
	}
	if err = c.SaveFile(header, destination); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrSaveFile")
		return nil, err
	}
	response, err := q.uploadService.CreateUpload(ctx, &models.Upload{
		StoreID:  storeID,
		Path:     path,
		URL:      q.config.PublicURL + "/uploads/" + filepath.ToSlash(path),
		MimeType: mimeType,
		Size:     header.Size,
		Purpose:  purpose,
	})
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateUpload")
	}
	return response, err
}

// record writes an entry of the activity log for the signed-in user; it never fails the request.
func record(c fiber.Ctx, activityService services.ActivityService, action string, description i18n.Text) {
	activityService.Record(c.Context(), &models.Activity{
		StoreID:   currentStoreID(c),
		UserID:    currentUserID(c),
		Action:    action,
		Text:      description,
		IPAddress: c.IP(),
		UserAgent: c.Get(fiber.HeaderUserAgent),
		CreatedAt: time.Now(),
	})
}

type uploadController struct {
	authService   *middlewares.Service
	uploader      *Uploader
	regionService services.RegionService
}

func NewUploadController(
	authService *middlewares.Service,
	uploader *Uploader,
	regionService services.RegionService,
) *uploadController {
	return &uploadController{
		authService:   authService,
		uploader:      uploader,
		regionService: regionService,
	}
}

// Mount registers POST /uploads and the region lookups.
func (q *uploadController) Mount(r fiber.Router) {
	private := r.Group("", q.authService.Middleware())
	private.Post("/uploads", q.V1Upload)
	private.Get("/regions/provinces", q.V1Provinces)
	private.Get("/regions/cities", q.V1Cities)
}

// V1Upload godoc
//
//	@Summary		Upload a file
//	@Description	Stores an image (JPG, PNG, WebP, GIF, SVG) or a PDF of at most 10 MB and returns its URL, to be sent in the attribute that refers to it (image_url, logo_url …).
//	@Tags			Files
//	@Accept			multipart/form-data
//	@Param			file	formData	file	true	"the file"
//	@Param			purpose	formData	string	false	"what it is for, e.g. product_image"
//	@Success		201		{object}	helper.Response{data=models.Upload}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/uploads [post]
func (q *uploadController) V1Upload(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "UploadController-V1Upload"
	response, err := q.uploader.Save(c, "file", c.FormValue("purpose"))
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrSave")
		return err
	}
	return helper.Created(c, response)
}

// V1Provinces godoc
//
//	@Summary	List provinces
//	@Tags		Regions
//	@Param		country_code	query		string	false	"country"	default(ID)
//	@Param		q				query		string	false	"search in name"
//	@Success	200				{object}	helper.Response{data=[]models.Region}
//	@Failure	401				{object}	helper.Response{data=helper.ErrorData}
//	@Router		/regions/provinces [get]
func (q *uploadController) V1Provinces(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "UploadController-V1Provinces"
	response, err := q.regionService.FindProvinces(ctx, c.Query("country_code", "ID"), c.Query("q"))
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindProvinces")
		return err
	}
	return helper.OK(c, response)
}

// V1Cities godoc
//
//	@Summary	List cities
//	@Tags		Regions
//	@Param		province_id	query		string	false	"only cities of this province (UUID)"
//	@Param		q			query		string	false	"search in name"
//	@Success	200			{object}	helper.Response{data=[]models.Region}
//	@Failure	401			{object}	helper.Response{data=helper.ErrorData}
//	@Router		/regions/cities [get]
func (q *uploadController) V1Cities(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "UploadController-V1Cities"
	provinceID, _ := sanitizers.QueryID(c, "province_id")
	response, err := q.regionService.FindCities(ctx, provinceID, c.Query("q"))
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindCities")
		return err
	}
	return helper.OK(c, response)
}
