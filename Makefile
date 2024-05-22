server:
	go build .\main.go ; .\main.exe -hour=23 -minute=0 -lead=true -consumer=true

integration_test:
	$env:START_HOUR="23"; $env:START_MINUTE="0"; go test ./integration_test/... > log
