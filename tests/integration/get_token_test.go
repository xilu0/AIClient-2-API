package integration

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

func TestGetToken(t *testing.T) {
	if os.Getenv("GET_TOKEN") != "true" {
		t.Skip("Set GET_TOKEN=true")
	}
	redisClient := getRedisClient(t)
	defer redisClient.Close()
	acc, token := getHealthyAccount(t, redisClient)
	info := map[string]string{
		"region": token.IDCRegion,
		"token":  token.AccessToken,
	}
	b, _ := json.Marshal(info)
	fmt.Println(string(b))
	_ = acc
}
