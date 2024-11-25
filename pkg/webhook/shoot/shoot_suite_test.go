package shoot_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestWebhookShoots(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Webhook shoot Suite")
}
