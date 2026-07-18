package generation

import (
	"fmt"
	"os"
	"strconv"

	"github.com/eqzhou/flyaimovie/internal/services/mediacache"
	"github.com/eqzhou/flyaimovie/internal/storage"
)

func cacheGeneratedFile(cache *mediacache.Service, store *storage.LocalStorage, organizationID uint, namespace string, key uint, kind, rel, publicURL, mimeType string) (string, string, string, int64, error) {
	if store == nil {
		return "", "", "", 0, fmt.Errorf("media storage is not configured")
	}
	hash, size, err := mediacache.HashFile(store.Abs(rel))
	if err != nil {
		return "", "", "", 0, err
	}
	if cache == nil {
		return rel, publicURL, hash, size, nil
	}
	object, reused, err := cache.Put(mediacache.PutInput{OrganizationID: organizationID, Namespace: namespace,
		Key: strconv.FormatUint(uint64(key), 10), ContentHash: hash, Kind: kind, LocalPath: rel, PublicURL: publicURL, MimeType: mimeType, Size: size})
	if err != nil {
		return "", "", "", 0, err
	}
	if reused && object.LocalPath != rel {
		if err := os.Remove(store.Abs(rel)); err != nil && !os.IsNotExist(err) {
			return "", "", "", 0, err
		}
	}
	return object.LocalPath, object.PublicURL, hash, size, nil
}
