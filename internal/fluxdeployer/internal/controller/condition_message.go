package controller

import "unicode/utf8"

// maxConditionMessageLength is the API server's limit for metav1.Condition messages.
const maxConditionMessageLength = 32768

// truncateMessage cuts a message copied from another object to the API limit, on a UTF-8 boundary.
func truncateMessage(message string) string {
	if len(message) <= maxConditionMessageLength {
		return message
	}
	cut := maxConditionMessageLength
	for cut > 0 && !utf8.RuneStart(message[cut]) {
		cut--
	}
	return message[:cut]
}
