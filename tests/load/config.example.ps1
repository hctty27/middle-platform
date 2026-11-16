# Copy this file to config.local.ps1 and fill in TEST-channel values.
# config.local.ps1 is ignored by Git and must never be committed.

$env:CONFIG_SUBSCRIBER_CODE = "replace-with-test-subscriber-code"
$env:CONFIG_SUBSCRIBER_SECRET = "replace-with-test-subscriber-secret-at-least-32-bytes"

$env:CORE_ENV = "test"
$env:CORE_APP_ID = "replace-with-test-channel-app-id"
$env:CORE_GAME_SECRET = "replace-with-test-game-secret"

$env:SDK_SERVICE_ENV = "test"
$env:SDK_SERVICE_APP_ID = "replace-with-test-channel-app-id"
$env:SDK_SERVICE_GAME_SECRET = "replace-with-test-game-secret"
$env:OPEN_ID_PREFIX = "loadtest"
# Only set true after confirming this TEST channel bypasses the external platform call.
$env:SDK_SERVICE_SKIP_USER_CHECK_CONFIRMED = "false"

$env:ORDER_ID_PREFIX = "MPLOAD"
$env:ORDER_AMOUNT = "1"
