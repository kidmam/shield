package core

import (
	"time"

	"github.com/jhunt/go-log"

	"github.com/starkandwayne/shield/db"
)

func (core *Core) updateStorageUsage() {
	log.Debugf("updating daily storage analytics per store")
	core.updateGlobalStorageUsage()
	log.Debugf("updating daily storage analytics per tenant")
	core.updateTenantStorageUsage()
}

func (core *Core) AreStoresHealthy() bool {
	stores, err := core.DB.GetAllStores(nil)
	if err != nil {
		log.Errorf("Failed to get stores for health tests: %s", err)
	}

	for _, store := range stores {
		if !store.Healthy {
			return false
		}
	}
	return true
}

func (core *Core) testStorage() {
	log.Debugf("testing health of cloud stores")

	stores, err := core.DB.GetAllStores(nil)
	if err != nil {
		log.Errorf("Failed to get stores for health tests: %s", err)
		return
	}

	inflight, err := core.DB.GetAllTasks(&db.TaskFilter{
		ForOp:        db.TestStoreOperation,
		SkipInactive: true,
	})
	if err != nil {
		log.Errorf("Failed to get in-flight health test tasks: %s", err)
		return
	}

	lookup := make(map[string]bool)
	for _, task := range inflight {
		lookup[task.StoreUUID.String()] = true
	}

	for _, store := range stores {
		if _, inqueue := lookup[store.UUID.String()]; inqueue {
			continue
		}

		_, err := core.DB.CreateTestStoreTask("system", store)
		if err != nil {
			log.Errorf("failed to create test store task: %s", err)
		}
	}
}

// DeltaIncrease calculates the delta in storage space over the period specified.
// It stores the number of bytes increased/decreased in the period specified in the stores table.
// Calculation is preformed by taking (total new archives created - any archives newly purged)
func (core *Core) DeltaIncrease(filter *db.ArchiveFilter) (int64, error) {
	delta_increase, err := core.DB.ArchiveStorageFootprint(&db.ArchiveFilter{
		ForStore:   filter.ForStore,
		ForTenant:  filter.ForTenant,
		Before:     filter.Before,
		After:      filter.After,
		WithStatus: []string{"valid"},
	})
	if err != nil {
		log.Errorf("Failed to get archive stats for daily storage statistics: %s", err)
		return -1, err
	}

	delta_purged, err := core.DB.ArchiveStorageFootprint(&db.ArchiveFilter{
		ForStore:      filter.ForStore,
		ForTenant:     filter.ForTenant,
		ExpiresBefore: filter.ExpiresBefore,
		ExpiresAfter:  filter.ExpiresAfter,
		WithStatus:    []string{"purged"},
	})
	if err != nil {
		log.Errorf("Failed to get archive stats for daily storage statistics: %s", err)
		return -1, err
	}
	return (delta_increase - delta_purged), nil
}

func (core *Core) updateGlobalStorageUsage() error {
	base := time.Now()
	threshold := base.Add(0 - time.Duration(24)*time.Hour)

	stores, err := core.DB.GetAllStores(nil)
	if err != nil {
		log.Errorf("Failed to get stores for daily storage statistics: %s", err)
		return err
	}

	for _, store := range stores {
		delta, err := core.DeltaIncrease(
			&db.ArchiveFilter{
				ForStore:      store.UUID.String(),
				Before:        &base,
				After:         &threshold,
				ExpiresBefore: &base,
				ExpiresAfter:  &threshold,
			},
		)
		if err != nil {
			return err
		}

		total_size, err := core.DB.ArchiveStorageFootprint(
			&db.ArchiveFilter{
				ForStore:   store.UUID.String(),
				WithStatus: []string{"valid"},
			},
		)
		if err != nil {
			log.Errorf("Failed to get archive stats for daily storage statistics: %s", err)
			return err
		}

		total_count, err := core.DB.CountArchives(
			&db.ArchiveFilter{
				ForStore:   store.UUID.String(),
				WithStatus: []string{"valid"},
			},
		)
		if err != nil {
			log.Errorf("Failed to get archive stats for daily storage statistics: %s", err)
			return err
		}

		store.DailyIncrease = delta
		store.StorageUsed = total_size
		store.ArchiveCount = total_count
		log.Debugf("updating store '%s' (%s) %d archives, %db storage used, %db increase",
			store.Name, store.UUID.String(), store.ArchiveCount, store.StorageUsed, store.DailyIncrease)
		err = core.DB.UpdateStore(store)
		if err != nil {
			log.Errorf("Failed to update stores with daily storage statistics: %s", err)
			return err
		}
	}
	return nil
}

func (core *Core) updateTenantStorageUsage() error {
	base := time.Now()
	threshold := base.Add(0 - time.Duration(24)*time.Hour)
	tenants, err := core.DB.GetAllTenants(nil)
	if err != nil {
		log.Errorf("Failed to get tenants for daily storage statistics: %s", err)
		return err
	}

	for _, tenant := range tenants {
		delta, err := core.DeltaIncrease(
			&db.ArchiveFilter{
				ForTenant:     tenant.UUID.String(),
				Before:        &base,
				After:         &threshold,
				ExpiresBefore: &base,
				ExpiresAfter:  &threshold,
			},
		)
		if err != nil {
			return err
		}

		total_size, err := core.DB.ArchiveStorageFootprint(
			&db.ArchiveFilter{
				ForTenant:  tenant.UUID.String(),
				WithStatus: []string{"valid"},
			},
		)
		if err != nil {
			log.Errorf("Failed to get archive stats for daily storage statistics: %s", err)
			return err
		}

		total_count, err := core.DB.CountArchives(
			&db.ArchiveFilter{
				ForTenant:  tenant.UUID.String(),
				WithStatus: []string{"valid"},
			},
		)
		if err != nil {
			log.Errorf("Failed to get archive stats for daily storage statistics: %s", err)
			return err
		}

		tenant.StorageUsed = total_size
		tenant.ArchiveCount = total_count
		tenant.DailyIncrease = delta

		log.Debugf("updating tenant '%s' (%s) %d archives, %db storage used, %db increase",
			tenant.Name, tenant.UUID.String(), tenant.ArchiveCount, tenant.StorageUsed, tenant.DailyIncrease)
		if _, err = core.DB.UpdateTenant(tenant); err != nil {
			log.Errorf("Failed to update tenant with daily storage statistics: %s", err)
			return err
		}
	}
	return nil
}
