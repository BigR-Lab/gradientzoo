package models

import (
	"database/sql"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/pborman/uuid"
	"gopkg.in/guregu/null.v3/zero"
	runner "gopkg.in/mgutz/dat.v1/sqlx-runner"
)

const MODEL_TABLE = "model"

type ModelDb struct {
	DB  *runner.DB
	Api *ApiCollection
}

//go:generate counterfeiter $GOFILE ModelApi
type ModelApi interface {
	ById(id interface{}) (*Model, error)
	ByIds(ids []interface{}) ([]*Model, error)
	Delete(id interface{}) error
	Save(*Model) error
	Hydrate([]*Model) error
	Truncate() error

	// TODO: Potentially this should be a separate interface
	ByUserId(userId string) ([]*Model, error)
	ByUserIdSlug(userId, slug string) (*Model, error)
	ByVisibility(visibility string, limit int, last string) ([]*Model, error)
	ByDownloads(visibility string, start, end time.Time, limit int, last string) ([]*Model, error)
}

func NewModelDb(db *runner.DB, api *ApiCollection) *ModelDb {
	return &ModelDb{
		DB:  db,
		Api: api,
	}
}

type Model struct {
	Id          string    `db:"id" json:"id"`
	UserId      string    `db:"user_id" json:"user_id"`
	Slug        string    `db:"slug" json:"slug"`
	Name        string    `db:"name" json:"name"`
	Description string    `db:"description" json:"description"`
	Visibility  string    `db:"visibility" json:"visibility"`
	Keep        int       `db:"keep" json:"keep"`
	Readme      string    `db:"readme" json:"-"`
	CreatedTime time.Time `db:"created_time" json:"created_time"`

	// Hydrated fields
	Downloads      *DownloadCounts `db:"-" json:"downloads,omitempty"`
	HydratedReadme zero.String     `db:"-" json:"readme,omitempty"`
}

func NewModel(userId, slug, name, description, visibility string, keep int) *Model {
	model := &Model{
		Id:          uuid.NewUUID().String(),
		UserId:      userId,
		Slug:        slug,
		Name:        name,
		Description: description,
		Visibility:  visibility,
		Keep:        keep,
		CreatedTime: time.Now().UTC(),
	}
	return model
}

func (db *ModelDb) ById(id interface{}) (*Model, error) {
	var model Model
	err := db.DB.
		Select("*").
		From(MODEL_TABLE).
		Where("id = $1", id).
		QueryStruct(&model)
	if err == sql.ErrNoRows {
		return nil, err
	}
	return &model, err
}

func (db *ModelDb) ByIds(ids []interface{}) ([]*Model, error) {
	if len(ids) == 0 {
		return []*Model{}, nil
	}
	var models []*Model
	err := db.DB.
		Select("*").
		From(MODEL_TABLE).
		Where("id IN $1", IdStrings(ids)).
		QueryStructs(&models)
	if models == nil {
		models = []*Model{}
	}
	return models, err
}

func (db *ModelDb) Delete(id interface{}) error {
	_, err := db.DB.
		DeleteFrom(MODEL_TABLE).
		Where("id = $1", id).
		Exec()
	return err
}

func (db *ModelDb) Save(model *Model) error {
	cols := []string{
		"id",
		"user_id",
		"slug",
		"name",
		"description",
		"visibility",
		"keep",
		"readme",
		"created_time",
	}
	vals := []interface{}{
		model.Id,
		model.UserId,
		model.Slug,
		model.Name,
		model.Description,
		model.Visibility,
		model.Keep,
		model.Readme,
		model.CreatedTime,
	}
	_, err := db.DB.
		Upsert(MODEL_TABLE).
		Columns(cols...).
		Values(vals...).
		Where("id = $1", model.Id).
		Exec()
	return err
}

func (db *ModelDb) Hydrate(models []*Model) error {
	modelIds := make([]string, 0, len(models))
	for _, model := range models {
		modelIds = append(modelIds, model.Id)
	}

	counts, err := db.Api.DownloadHour.CountsByModels(modelIds)
	if err != nil {
		return err
	}

	for _, model := range models {
		c := counts[model.Id]
		model.Downloads = &c
		model.HydratedReadme = zero.StringFrom(model.Readme)
	}
	return nil
}

func (db *ModelDb) Truncate() error {
	_, err := db.DB.DeleteFrom(MODEL_TABLE).Exec()
	return err
}

// -

func (db *ModelDb) ByUserId(userId string) ([]*Model, error) {
	var models []*Model
	err := db.DB.
		Select("*").
		From(MODEL_TABLE).
		Where("user_id = $1", userId).
		QueryStructs(&models)
	if models == nil {
		models = []*Model{}
	}
	return models, err
}

func (db *ModelDb) ByUserIdSlug(userId, slug string) (*Model, error) {
	var model Model
	err := db.DB.
		Select("*").
		From(MODEL_TABLE).
		Where("user_id = $1 AND slug = $2", userId, slug).
		QueryStruct(&model)
	if err == sql.ErrNoRows {
		return nil, err
	}
	return &model, err
}

func (db *ModelDb) ByVisibility(visibility string, limit int, last string) ([]*Model, error) {
	if last != "" {
		log.Error("ByVisibility does not yet handle pagination, 'last' param ignored")
	}
	var models []*Model
	err := db.DB.
		Select("*").
		From(MODEL_TABLE).
		Where("visibility = $1", visibility).
		OrderBy("created_time DESC").
		Limit(uint64(limit)).
		QueryStructs(&models)
	if models == nil {
		models = []*Model{}
	}
	return models, err
}

func (db *ModelDb) ByDownloads(visibility string, start, end time.Time, limit int, last string) ([]*Model, error) {
	if last != "" {
		log.Error("ByDownloads does not yet handle pagination, 'last' param ignored")
	}
	sql := `
	SELECT
		M.*
	FROM download_hour DH
	LEFT JOIN file F ON (F.id = DH.file_id)
	LEFT JOIN model M ON (M.id = F.model_id)
	WHERE M.visibility = $1
	GROUP BY M.id,
					 M.user_id,
					 M.slug,
					 M.name,
					 M.description,
					 M.visibility,
					 M.keep,
					 M.readme,
					 M.created_time
	ORDER BY COALESCE(SUM(CASE WHEN DH.hour >= $2 AND DH.hour < $3 THEN DH.downloads ELSE 0 END)) DESC
	LIMIT $4
	`
	var models []*Model
	err := db.DB.SQL(sql, visibility, start, end, limit).QueryStructs(&models)
	if models == nil {
		models = []*Model{}
	}
	return models, err
}
