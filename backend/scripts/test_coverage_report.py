import unittest

import coverage_report as report


class CoverageReportTests(unittest.TestCase):
    def test_ai_module_cannot_be_missing_or_below_coverage_gate(self):
        packages = {name: {'percent': 100} for name in report.PACKAGE_MINIMUMS}
        packages.pop('dicho/internal/modules/mealai', None)
        summary = {'total': {'percent': 100}, 'packages': packages}
        core = {'Allocate': 100, 'Missing': 100, 'Score': 100}
        self.assertTrue(any('modules/mealai' in f for f in report.enforce(summary, core)))
        packages['dicho/internal/modules/mealai'] = {'percent': 84.99}
        self.assertTrue(any('modules/mealai' in f for f in report.enforce(summary, core)))

    def test_duplicate_blocks_preserve_denominator_and_shared_execution(self):
        profile = "mode: atomic\ndicho/internal/app/a.go:1.1,2.2 3 0\ndicho/internal/app/a.go:1.1,2.2 3 2\ndicho/cmd/server/main.go:1.1,2.2 1 0\n"
        result = report.summarize(report.parse_profile(profile))
        self.assertEqual(result['total']['statements'], 4)
        self.assertEqual(result['total']['covered'], 3)
        self.assertEqual(result['packages']['dicho/internal/app']['percent'], 100)
        self.assertEqual(result['packages']['dicho/cmd/server']['percent'], 0)

    def test_corrupt_or_conflicting_blocks_are_rejected(self):
        for profile in ["not a profile", "mode: atomic\ninvalid", "mode: atomic\nx.go:1.1,2.2 -1 1\n", "mode: atomic\nx.go:1.1,2.2 1 1\nx.go:1.1,2.2 2 1\n"]:
            with self.subTest(profile=profile):
                with self.assertRaises(ValueError):
                    report.parse_profile(profile)

    def test_gates_fail_missing_sources_or_low_core_without_excluding_entrypoints(self):
        summary = {'total': {'percent': 84}, 'packages': {}}
        failures = report.enforce(summary, {})
        self.assertTrue(any('global' in f for f in failures))
        self.assertTrue(any('dicho/internal/app' in f for f in failures))
        self.assertTrue(any('Allocate' in f for f in failures))
        packages = {name: {'percent': 85} for name in report.PACKAGE_MINIMUMS}
        packages['dicho/internal/kernel']['percent'] = 90
        self.assertEqual(report.enforce({'total': {'percent': 85}, 'packages': packages}, {'Allocate': 90, 'Missing': 90, 'Score': 90}), [])

    def test_core_function_parser_keeps_exact_function_names(self):
        self.assertEqual(report.parse_core_functions('dicho/internal/modules/inventory/domain.go:21:\tAllocate\t94.7%\ndicho/internal/modules/planning/algorithms.go:51:\tMissing\t95.0%\ndicho/internal/modules/planning/algorithms.go:112:\tScore\t97.7%\n'), {'Allocate': 94.7, 'Missing': 95, 'Score': 97.7})

    def test_skipped_tests_and_package_failures_cannot_be_reported_as_pass(self):
        counts = report.test_results('{"Action":"pass","Test":"TestOne"}\n{"Action":"skip","Test":"TestSkipped"}\n{"Action":"fail","Package":"dicho/broken"}\n')
        self.assertEqual(counts, {'pass': 1, 'fail': 0, 'skip': 1, 'packagesFailed': ['dicho/broken']})
        self.assertTrue(any('tests' in failure for failure in report.enforce({'total': {'percent': 100}, 'packages': {}}, {}, counts)))


if __name__ == '__main__':
    unittest.main()
