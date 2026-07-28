// Ponte para o Windows. No WSL o lado Linux costuma não ter daemon de
// notificação: o barramento existe, mas ninguém atende
// org.freedesktop.Notifications, e `notify-send` e `kdialog` falham junto —
// ou seja, o beeep esgota todas as saídas e o aviso se perde justamente no
// ambiente em que o TCC é desenvolvido. Quem tem uma área de trabalho ali é o
// Windows, e ela está a um `powershell.exe` de distância.
package notify

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// appID do PowerShell: o toast precisa ser emitido em nome de um aplicativo
// registrado, e este existe sempre — é o processo que estamos chamando.
const appID = `{1AC14E77-02E7-4E5D-B744-2EB1AE5198B7}\WindowsPowerShell\v1.0\powershell.exe`

// wslTimeout: subir o PowerShell custa algumas centenas de milissegundos, e o
// disparo já roda fora do laço da TUI — mas não pode ficar pendurado.
const wslTimeout = 10 * time.Second

// orWindows só entra em cena depois de o beeep falhar, e só no WSL.
func orWindows(err error, title, body string, urgent bool) error {
	if err == nil || !insideWSL() {
		return err
	}
	return windowsToast(title, body, urgent)
}

var insideWSL = sync.OnceValue(func() bool {
	if os.Getenv("WSL_DISTRO_NAME") != "" {
		return true
	}
	version, readErr := os.ReadFile("/proc/version")
	return readErr == nil && strings.Contains(strings.ToLower(string(version)), "microsoft")
})

func windowsToast(title, body string, urgent bool) error {
	duration := "short"
	if urgent {
		duration = "long" // o despertador espera o usuário voltar
	}
	script := `
[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType=WindowsRuntime] | Out-Null
$xml = [Windows.UI.Notifications.ToastNotificationManager]::GetTemplateContent([Windows.UI.Notifications.ToastTemplateType]::ToastText02)
$text = $xml.GetElementsByTagName('text')
$text.Item(0).AppendChild($xml.CreateTextNode('` + psQuote(title) + `')) | Out-Null
$text.Item(1).AppendChild($xml.CreateTextNode('` + psQuote(body) + `')) | Out-Null
$xml.DocumentElement.SetAttribute('duration', '` + duration + `')
$toast = [Windows.UI.Notifications.ToastNotification]::new($xml)
[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier('` + appID + `').Show($toast)`

	ctx, cancel := context.WithTimeout(context.Background(), wslTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.Stdout, cmd.Stderr = nil, nil
	return cmd.Run()
}

// psQuote escapa para dentro de uma string entre aspas simples do PowerShell,
// onde a aspa se escapa duplicando. O texto entra no XML por CreateTextNode,
// então a marcação em si não corre risco; as quebras de linha viram espaço
// para o script continuar de uma linha só.
func psQuote(s string) string {
	s = strings.NewReplacer("\r", " ", "\n", " ").Replace(s)
	return strings.ReplaceAll(s, "'", "''")
}
