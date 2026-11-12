package services

import (
	"context"
	"log/slog"
	"maps"
	"regexp"
	"strings"
	"uuid"

	"github.com/goccy/go-json"
	"github.com/jackc/pgx/v5"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/models"
	"github.com/roysitumorang/lestari-backend/repositories"
)

// slugPattern is a subdomain or link-page username: 3–40 lowercase letters, numbers or dashes.
var slugPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{1,38}[a-z0-9])?$`)

// slugAttributes names the attribute of a setting that must be unique across all branches.
var slugAttributes = map[string]string{
	models.SettingOnlineStore: "subdomain",
	models.SettingLinkPage:    "username",
}

type SettingService interface {
	// FindSetting fills `into` (a pointer from models.NewSetting, or to the same type) with the defaults of the
	// setting overlaid by what the branch saved.
	FindSetting(ctx context.Context, tx pgx.Tx, branchID uuid.UUID, key string, into any) error
	// UpdateSetting changes the attributes of the patch and returns the setting as it is afterwards. When no
	// value differs from the stored one nothing is written.
	UpdateSetting(ctx context.Context, tx pgx.Tx, request *models.SettingUpdate) (any, error)
	// SlugAvailable tells whether a link-page username is well-formed and free.
	SlugAvailable(ctx context.Context, branchID uuid.UUID, key, value string) (bool, error)
	FindLinkPageVisits(ctx context.Context, branchID uuid.UUID, limit, page int64) ([]*models.LinkPageVisit, int64, error)
	// FindMenuItems lists the products of the printable menu of the QR ordering page.
	FindMenuItems(ctx context.Context, branchID uuid.UUID) ([]*models.MenuItem, error)
	// PublicURL is the address customers reach the branch at: its link page, its online store or its id.
	PublicURL(ctx context.Context, branchID uuid.UUID, baseURL string) (string, error)
}

type settingService struct {
	settingRepository repositories.SettingRepository
}

func NewSettingService(
	settingRepository repositories.SettingRepository,
) SettingService {
	return &settingService{
		settingRepository: settingRepository,
	}
}

// marshalSetting renders a setting as the document that is stored: the typed value overlaid by overlays, in
// order. A nil value in an overlay clears an attribute.
func marshalSetting(setting any, overlays ...map[string]any) (json.RawMessage, error) {
	raw, err := json.Marshal(setting)
	if err != nil {
		return nil, err
	}
	document := map[string]any{}
	if err = json.Unmarshal(raw, &document); err != nil {
		return nil, err
	}
	for _, overlay := range overlays {
		maps.Copy(document, overlay)
	}
	return json.Marshal(document)
}

func (q *settingService) FindSetting(ctx context.Context, tx pgx.Tx, branchID uuid.UUID, key string, into any) error {
	ctxt := "SettingService-FindSetting"
	stored, err := q.settingRepository.FindSetting(ctx, tx, branchID, key)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindSetting")
		return err
	}
	defaults := models.NewSetting(key)
	if defaults == nil {
		return models.ErrSettingUnknown
	}
	// defaults first, so `into` may be a fresh zero value
	raw, err := json.Marshal(defaults)
	if err == nil {
		err = json.Unmarshal(raw, into)
	}
	if err == nil && stored != nil {
		err = json.Unmarshal(stored, into)
	}
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrUnmarshal")
	}
	return err
}

func (q *settingService) UpdateSetting(ctx context.Context, tx pgx.Tx, request *models.SettingUpdate) (any, error) {
	ctxt := "SettingService-UpdateSetting"
	if attribute, ok := slugAttributes[request.Key]; ok {
		if slug, sent := request.Patch[attribute].(string); sent && slug != "" {
			slug = strings.ToLower(slug)
			request.Patch[attribute] = slug
			if !slugPattern.MatchString(slug) {
				return nil, helper.Invalid(attribute, "Use 3–40 lowercase letters, numbers or dashes.")
			}
			taken, err := q.settingRepository.SlugTaken(ctx, request.BranchID, request.Key, attribute, slug)
			if err != nil {
				helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrSlugTaken")
				return nil, err
			}
			if taken {
				return nil, helper.Invalid(attribute, "This name is already taken.")
			}
		}
	}
	defaults := models.NewSetting(request.Key)
	if defaults == nil {
		return nil, models.ErrSettingUnknown
	}
	stored, err := q.settingRepository.FindSetting(ctx, tx, request.BranchID, request.Key)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindSetting")
		return nil, err
	}
	saved := map[string]any{}
	if stored != nil {
		if err = json.Unmarshal(stored, &saved); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrUnmarshal")
			return nil, err
		}
	}
	current, err := marshalSetting(defaults, saved)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrMarshalSetting")
		return nil, err
	}
	value, err := marshalSetting(defaults, saved, request.Patch)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrMarshalSetting")
		return nil, err
	}
	response := models.NewSetting(request.Key)
	if err = json.Unmarshal(value, response); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrUnmarshal")
		return nil, err
	}
	// both documents are marshalled from maps, so equal settings are equal bytes
	if stored != nil && string(current) == string(value) {
		return response, nil
	}
	if err = q.settingRepository.SaveSetting(ctx, tx, request.BranchID, request.Key, value, request.At); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrSaveSetting")
		return nil, err
	}
	if store, ok := response.(*models.StoreSetting); ok && store.Name != "" {
		// the store profile names the branch and, at the head office, the store
		if err = q.settingRepository.SyncBranchProfile(ctx, tx, request.BranchID, store.Name, deref(store.Phone), deref(store.Address), request.At); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrSyncBranchProfile")
			return nil, err
		}
		if request.BranchID == request.HeadOfficeID {
			if err = q.settingRepository.SyncStoreName(ctx, tx, request.StoreID, store.Name, request.At); err != nil {
				helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrSyncStoreName")
				return nil, err
			}
		}
	}
	return response, nil
}

func (q *settingService) SlugAvailable(ctx context.Context, branchID uuid.UUID, key, value string) (bool, error) {
	ctxt := "SettingService-SlugAvailable"
	value = strings.ToLower(strings.TrimSpace(value))
	if !slugPattern.MatchString(value) {
		return false, nil
	}
	taken, err := q.settingRepository.SlugTaken(ctx, branchID, key, slugAttributes[key], value)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrSlugTaken")
	}
	return !taken, err
}

func (q *settingService) FindLinkPageVisits(ctx context.Context, branchID uuid.UUID, limit, page int64) ([]*models.LinkPageVisit, int64, error) {
	ctxt := "SettingService-FindLinkPageVisits"
	response, total, err := q.settingRepository.FindLinkPageVisits(ctx, branchID, limit, page)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindLinkPageVisits")
	}
	return response, total, err
}

func (q *settingService) FindMenuItems(ctx context.Context, branchID uuid.UUID) ([]*models.MenuItem, error) {
	ctxt := "SettingService-FindMenuItems"
	var qrOrder models.QROrderSetting
	if err := q.FindSetting(ctx, nil, branchID, models.SettingQROrder, &qrOrder); err != nil {
		return nil, err
	}
	response, err := q.settingRepository.FindMenuItems(ctx, branchID, qrOrder.CategoryIDs)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindMenuItems")
	}
	return response, err
}

func (q *settingService) PublicURL(ctx context.Context, branchID uuid.UUID, baseURL string) (string, error) {
	var linkPage models.LinkPageSetting
	if err := q.FindSetting(ctx, nil, branchID, models.SettingLinkPage, &linkPage); err != nil {
		return "", err
	}
	if linkPage.Username != "" {
		return baseURL + "/l/" + linkPage.Username, nil
	}
	var onlineStore models.OnlineStoreSetting
	if err := q.FindSetting(ctx, nil, branchID, models.SettingOnlineStore, &onlineStore); err != nil {
		return "", err
	}
	if subdomain := deref(onlineStore.Subdomain); subdomain != "" {
		return baseURL + "/shop/" + subdomain, nil
	}
	return baseURL + "/s/" + branchID.String(), nil
}
