package extenders

import (
	"fmt"

	"github.com/ffenix113/zigbee_home/cli/types"
	"github.com/ffenix113/zigbee_home/cli/types/devicetree"
	"github.com/ffenix113/zigbee_home/cli/types/generator"
)

var _ generator.Extender = Button{}
var _ devicetree.Applier = Button{}

type Button struct {
	generator.SimpleExtender

	Instances []types.Pin
}

func NewButtons(instances ...types.Pin) generator.Extender {
	for i := range instances {
		if instances[i].ID == "" {
			instances[i].ID = instances[i].Label()
		}
	}

	return Button{
		Instances: instances,
	}
}

func (l Button) Template() string {
	return "peripherals/buttons"
}

func (l Button) Includes() []string {
	return []string{"zephyr/drivers/gpio.h"}
}

func (l Button) ApplyOverlay(dt *devicetree.DeviceTree) error {
	for _, instance := range l.Instances {
		ledInstance := devicetree.NewButton(instance)
		if err := ledInstance.AttachSelf(dt); err != nil {
			return fmt.Errorf("attach button: %w", err)
		}
	}

	return nil
}
