import unittest

from preview import update_overrides, validate_report


class WorkflowContracts(unittest.TestCase):
    def test_update_preserves_other_builds_and_direct_images_without_provenance(self):
        composition = {'overrides': {
            'pricing': {'build_id': 'old', 'image': 'old-image', 'source': {'revision': 'old'}},
            'gateway': {'image': 'registry/gateway@sha256:abc'},
            'service-a': {'build_id': 'another-build', 'image': 'resolved', 'source': {'revision': 'other'}},
        }}
        self.assertEqual(update_overrides(composition, 'pricing', 'new'), {
            'pricing': {'build_id': 'new'}, 'gateway': {'image': 'registry/gateway@sha256:abc'},
            'service-a': {'build_id': 'another-build'},
        })
        self.assertEqual(composition['overrides']['pricing']['build_id'], 'old')

    def test_artifact_must_match_successful_run_attempt_and_source(self):
        run = {'conclusion': 'success', 'run_attempt': 2, 'head_sha': 'a' * 40}
        report = {'run_id': '123', 'attempt': 2, 'revision': 'a' * 40, 'component': 'pricing'}
        validate_report(report, run, '123', 'pricing')
        for field, value in [('run_id', '124'), ('attempt', 1), ('revision', 'b' * 40), ('component', 'gateway')]:
            with self.subTest(field=field), self.assertRaises(RuntimeError):
                validate_report({**report, field: value}, run, '123', 'pricing')
        with self.assertRaises(RuntimeError):
            validate_report(report, {**run, 'conclusion': 'failure'}, '123', 'pricing')


if __name__ == '__main__':
    unittest.main()
