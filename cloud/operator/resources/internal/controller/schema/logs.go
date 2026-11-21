package schema

import (
	"bufio"
	"context"
	"regexp"

	corev1 "k8s.io/api/core/v1"
	kubernetes "k8s.io/client-go/kubernetes"
)

func (clientsetLogReader *ClientsetLogReader) GetUpgradeResult(
	container string,
	context context.Context,
	pod *corev1.Pod,
) (string, string, error) {
	stream, error := clientsetLogReader.Clientset.CoreV1().Pods(
		pod.Namespace,
	).GetLogs(
		pod.Name, &corev1.PodLogOptions{Container: container},
	).Stream(context)

	if error != nil {
		return "", "", error
	}

	defer stream.Close()

	return parseUpgradeResult(bufio.NewScanner(stream))
}

func parseUpgradeResult(scanner *bufio.Scanner) (string, string, error) {
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)

	result := ""
	upgradeType := ""

	for scanner.Scan() {
		matches := upgradeResultPattern.FindStringSubmatch(scanner.Text())

		if matches != nil {
			upgradeType = matches[1]
			result = matches[2]
		}
	}

	return result, upgradeType, scanner.Err()
}

// ClientsetLogReader reads the result of a database upgrade from the
// UpgradeRecorder line that DXP logs when an upgrade finishes, for example
// "Major upgrade finished with result failure".
type ClientsetLogReader struct {
	Clientset kubernetes.Interface
}

var upgradeResultPattern = regexp.MustCompile(
	`UpgradeRecorder.*\b(\w+) upgrade finished with result (\w+)`,
)
