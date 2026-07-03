package internalauth

// MockVerifier は Verifier のテスト用モック。
type MockVerifier struct {
	VerifyFn func(token string) (string, error)
}

var _ Verifier = (*MockVerifier)(nil)

// Verify はトークンを検証してプレイヤー ID を返す。
func (m *MockVerifier) Verify(token string) (string, error) {
	if m.VerifyFn == nil {
		// 未設定のまま呼ばれた場合は panic で意図しない呼び出しを検出する
		panic("internalauth: MockVerifier.Verify called without VerifyFn")
	}
	return m.VerifyFn(token)
}
