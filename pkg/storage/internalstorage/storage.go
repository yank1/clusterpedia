package internalstorage

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	pediainternal "github.com/clusterpedia-io/clusterpedia/pkg/apis/pedia"
	"github.com/clusterpedia-io/clusterpedia/pkg/storage"
)

type Resource struct {
	ID uint `gorm:"primaryKey"`

	Group    string `gorm:"size:63;not null;uniqueIndex:uni_group_version_resource_cluster_namespace_name"`
	Version  string `gorm:"size:15;not null;uniqueIndex:uni_group_version_resource_cluster_namespace_name"`
	Resource string `gorm:"size:63;not null;uniqueIndex:uni_group_version_resource_cluster_namespace_name"`
	Kind     string `gorm:"size:63;not null"`

	Cluster         string    `gorm:"size:253;not null;uniqueIndex:uni_group_version_resource_cluster_namespace_name,length:100"`
	Namespace       string    `gorm:"size:253;not null;uniqueIndex:uni_group_version_resource_cluster_namespace_name,length:50"`
	Name            string    `gorm:"size:253;not null;uniqueIndex:uni_group_version_resource_cluster_namespace_name,length:100"`
	UID             types.UID `gorm:"size:36;not null"`
	ResourceVersion string    `gorm:"size:30;not null"`

	Object datatypes.JSON `gorm:"not null"`

	CreatedAt time.Time `gorm:"not null"`
	SyncedAt  time.Time `gorm:"not null;autoUpdateTime"`
	DeletedAt sql.NullTime
}

// SelectedResource used to select specific fields
type SelectedResource struct {
}

func (res Resource) GroupVersionResource() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    res.Group,
		Version:  res.Version,
		Resource: res.Resource,
	}
}

type StorageFactory struct {
	db *gorm.DB
}

func (s *StorageFactory) NewResourceStorage(config *storage.ResourceStorageConfig) (storage.ResourceStorage, error) {
	return &ResourceStorage{
		db:    s.db,
		codec: config.Codec,

		storageGroupResource: config.StorageGroupResource,
		storageVersion:       config.StorageVersion,
		memoryVersion:        config.MemoryVersion,
	}, nil
}

func (s *StorageFactory) NewCollectionResourceStorage(cr *pediainternal.CollectionResource) (storage.CollectionResourceStorage, error) {
	if _, ok := collectionResources[cr.Name]; !ok {
		return nil, fmt.Errorf("not support collection resource: %s", cr.Name)
	}

	return &CollectionResourceStorage{
		db:                 s.db,
		collectionResource: cr.DeepCopy(),
	}, nil
}

func (f *StorageFactory) GetResourceVersions(ctx context.Context, cluster string) (map[schema.GroupVersionResource]map[string]interface{}, error) {
	var resources []Resource
	result := f.db.WithContext(ctx).
		Select("group", "version", "resource", "namespace", "name", "resource_version").
		Where(&Resource{Cluster: cluster}).
		Find(&resources)

	if result.Error != nil {
		return nil, InterpreError(cluster, result.Error)
	}

	resourceversions := make(map[schema.GroupVersionResource]map[string]interface{})
	for _, resource := range resources {
		gvr := resource.GroupVersionResource()
		versions := resourceversions[gvr]
		if versions == nil {
			versions = make(map[string]interface{})
			resourceversions[gvr] = versions
		}

		key := resource.Name
		if resource.Namespace != "" {
			key = resource.Namespace + "/" + resource.Name
		}
		versions[key] = resource.ResourceVersion
	}
	return resourceversions, nil
}

func (f *StorageFactory) CleanCluster(ctx context.Context, cluster string) error {
	result := f.db.WithContext(ctx).Where(&Resource{Cluster: cluster}).Delete(Resource{})
	return InterpreError(cluster, result.Error)
}

func (s *StorageFactory) CleanClusterResource(ctx context.Context, cluster string, gvr schema.GroupVersionResource) error {
	resource := Resource{
		Cluster:  cluster,
		Group:    gvr.Group,
		Resource: gvr.Resource,
		Version:  gvr.Version,
	}

	result := s.db.Where(&resource).Delete(&Resource{})
	return InterpreError(fmt.Sprintf("%s/%s", cluster, gvr), result.Error)
}

func (s *StorageFactory) GetCollectionResources(ctx context.Context) ([]*pediainternal.CollectionResource, error) {
	var crs []*pediainternal.CollectionResource
	for _, cr := range collectionResources {
		crs = append(crs, cr.DeepCopy())
	}
	return crs, nil
}

var collectionResources = map[string]pediainternal.CollectionResource{
	"workloads": {
		ObjectMeta: metav1.ObjectMeta{
			Name: "workloads",
		},
		ResourceTypes: []pediainternal.CollectionResourceType{
			{
				Group:    "apps",
				Resource: "deployments",
			},
			{
				Group:    "apps",
				Resource: "daemonsets",
			},
			{
				Group:    "apps",
				Resource: "statefulsets",
			},
		},
	},
}
