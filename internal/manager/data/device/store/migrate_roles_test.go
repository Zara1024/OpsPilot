package store

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	driver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	model "github.com/Zara1024/OpsPilot/internal/manager/model/device"
)

func TestMigrateUpgradesDeviceRolesSQLite(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	testDeviceRolesUpgrade(t, db)
}

func TestMigrateUpgradesDeviceRolesMySQL(t *testing.T) {
	dsn := os.Getenv("OPSPILOT_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("set OPSPILOT_TEST_MYSQL_DSN to run MySQL upgrade regression")
	}
	cfg, err := driver.ParseDSN(dsn)
	if err != nil {
		t.Fatal("invalid OPSPILOT_TEST_MYSQL_DSN")
	}
	cfg.DBName = ""
	cfg.ParseTime = true
	admin, err := gorm.Open(mysql.Open(cfg.FormatDSN()), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	adminSQL, err := admin.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = adminSQL.Close() })
	cfg.DBName = "ongrid_device_test_" + uuid.New().String()
	if err := admin.Exec("CREATE DATABASE `" + cfg.DBName + "`").Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := admin.Exec("DROP DATABASE `" + cfg.DBName + "`").Error; err != nil {
			t.Errorf("remove test database: %v", err)
		}
	})
	db, err := gorm.Open(mysql.Open(cfg.FormatDSN()), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	testDeviceRolesUpgrade(t, db)
}

func testDeviceRolesUpgrade(t *testing.T, db *gorm.DB) {
	t.Helper()
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&model.Device{}); err != nil {
		t.Fatal(err)
	}
	if db.Dialector.Name() == "mysql" {
		if err := db.Exec("ALTER TABLE devices DROP CHECK chk_devices_roles, ADD CONSTRAINT chk_devices_roles CHECK (roles BETWEEN 0 AND 15)").Error; err != nil {
			t.Fatal(err)
		}
	} else {
		var ddl string
		if err := db.Raw("SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'devices'").Scan(&ddl).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Migrator().DropTable(&model.Device{}); err != nil {
			t.Fatal(err)
		}
		if err := db.Exec(strings.ReplaceAll(ddl, "roles BETWEEN 0 AND 31", "roles BETWEEN 0 AND 15")).Error; err != nil {
			t.Fatal(err)
		}
	}
	old := sampleDevice("existing-host")
	old.Roles = model.RoleBitServer
	if err := db.Create(old).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(old).Update("roles", model.RoleBitServer|model.RoleBitGPU).Error; err == nil {
		t.Fatal("legacy constraint accepted GPU role")
	}
	for i := 0; i < 2; i++ {
		if err := Migrate(db); err != nil {
			t.Fatalf("migration %d: %v", i+1, err)
		}
		var got model.Device
		if err := db.First(&got, old.ID).Error; err != nil || got.Fingerprint != old.Fingerprint || got.Roles != model.RoleBitServer {
			t.Fatalf("legacy device changed: %+v, error: %v", got, err)
		}
		for _, index := range []string{"idx_devices_roles", "idx_devices_fingerprint", "idx_devices_node_id"} {
			if !db.Migrator().HasIndex(&model.Device{}, index) {
				t.Fatalf("missing index %s after migration", index)
			}
		}
		gpu := sampleDevice(fmt.Sprintf("gpu-host-%d", i))
		gpu.Roles = model.RolesAllKnownBits
		if err := db.Create(gpu).Error; err != nil {
			t.Fatalf("create GPU device after migration: %v", err)
		}
		if err := db.Model(gpu).Update("roles", uint8(1<<5)).Error; err == nil {
			t.Fatal("reserved role bit passed the constraint")
		}
	}
	var count int64
	if err := db.Model(&model.Device{}).Count(&count).Error; err != nil || count != 3 {
		t.Fatalf("preserved devices = %d, want 3; error: %v", count, err)
	}
}
