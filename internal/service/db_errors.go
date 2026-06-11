package service

import "github.com/ShukeBta/MediaStationGo/internal/repository"

func IsTransientDatabaseLock(err error) bool {
	return repository.IsSQLiteBusyError(err)
}
