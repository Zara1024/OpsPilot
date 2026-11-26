package store

import (
	"reflect"
	"strings"
	"testing"

	model "github.com/Zara1024/OpsPilot/internal/manager/model/oncall"
)

func TestMigrateModels_MySQLNoTextOrJSONDefault(t *testing.T) {
	models := []any{
		model.Schedule{},
		model.Rotation{},
		model.Override{},
		model.Route{},
		model.EscalationPolicy{},
		model.UserNotificationRule{},
		model.UserCalendarToken{},
		model.HandoffLog{},
		model.VoiceConfigRecord{},
		model.ChannelConfigRecord{},
		model.WarRoom{},
		model.EscalationTask{},
		model.NotificationLog{},
		model.CalendarDay{},
		model.MaintenanceWindow{},
	}

	for _, m := range models {
		typ := reflect.TypeOf(m)
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			tag := strings.ToLower(field.Tag.Get("gorm"))
			if tag == "" {
				continue
			}

			parts := strings.Split(tag, ";")
			hasRestrictedType := false
			hasDefault := false
			for _, part := range parts {
				p := strings.TrimSpace(part)
				switch p {
				case "type:text", "type:mediumtext", "type:longtext", "type:json", "type:blob", "type:mediumblob", "type:longblob", "type:geometry":
					hasRestrictedType = true
				}
				if strings.HasPrefix(p, "type:") && strings.Contains(p, "blob") {
					hasRestrictedType = true
				}
				if strings.HasPrefix(p, "default:") {
					hasDefault = true
				}
			}

			if hasRestrictedType && hasDefault {
				t.Errorf("%s.%s has MySQL-incompatible gorm tag %q: TEXT/BLOB/JSON/GEOMETRY columns cannot have a default value", typ.Name(), field.Name, field.Tag.Get("gorm"))
			}
		}
	}
}
