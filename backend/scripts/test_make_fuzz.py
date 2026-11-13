import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


class FuzzCommandTests(unittest.TestCase):
    def run_target(self, exit_code):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            backend = root / "backend"
            backend.mkdir()
            shutil.copyfile(Path(__file__).resolve().parents[1] / "Makefile", backend / "Makefile")
            tools = root / "tools"
            tools.mkdir()
            go = tools / "go"
            go.write_text("#!/bin/sh\nprintf 'controlled fuzz result\\n'\nexit " + str(exit_code) + "\n")
            go.chmod(0o700)
            env = dict(os.environ, PATH=str(tools) + os.pathsep + os.environ["PATH"])
            result = subprocess.run(["make", "fuzz"], cwd=backend, env=env, text=True,
                                    stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
            logs = {p.name: p.read_text() for p in (root / "output/backend-qa").glob("fuzz-*.log")}
            return result, logs

    def test_failing_campaign_returns_failure_and_stops_following_campaigns(self):
        result, logs = self.run_target(7)
        self.assertNotEqual(result.returncode, 0, result.stdout)
        self.assertEqual(set(logs), {"fuzz-quantity.log"}, result.stdout)
        self.assertIn("controlled fuzz result", logs["fuzz-quantity.log"])

    def test_successful_campaigns_keep_all_five_logs(self):
        result, logs = self.run_target(0)
        self.assertEqual(result.returncode, 0, result.stdout)
        self.assertEqual(set(logs), {"fuzz-quantity.log", "fuzz-json.log", "fuzz-fefo.log",
                                     "fuzz-missing.log", "fuzz-dates.log"})


if __name__ == "__main__":
    unittest.main()
