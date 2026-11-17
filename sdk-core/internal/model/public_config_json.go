package model

import "encoding/json"

// EncodePublicConfig preserves the wire envelope, optional tsKey and trailing newline.
func EncodePublicConfig(value ConfigResponse) (string, error) {
	body, err := json.Marshal(struct {
		Code int `json:"code"`
		Message string `json:"message"`
		Data ConfigResponse `json:"data"`
	}{Code: 0, Message: "success", Data: value})
	if err != nil {
		return "", err
	}
	return string(body) + "\n", nil
}
