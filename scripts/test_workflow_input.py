import plistlib
import unittest
from pathlib import Path


class SearchInputTests(unittest.TestCase):
    def test_search_keyword_enters_suggestion_filter(self):
        path = Path(__file__).resolve().parents[1] / "workflow" / "info.plist"
        with path.open("rb") as source:
            workflow = plistlib.load(source)
        inputs = [
            obj for obj in workflow["objects"]
            if obj["config"].get("keyword") == "{var:search_keyword}"
        ]
        self.assertEqual(len(inputs), 1)
        entry = inputs[0]
        self.assertEqual(entry["type"], "alfred.workflow.input.scriptfilter")
        self.assertEqual(entry["config"]["argumenttype"], 1)  # Argument Optional, so entering the keyword starts warmup
        self.assertTrue(entry["config"]["withspace"])
        self.assertEqual(entry["config"]["scriptfile"], "googlesuggest")

        objects = {obj["uid"]: obj for obj in workflow["objects"]}
        edges = workflow["connections"]
        branch = objects[edges[entry["uid"]][0]["destinationuid"]]
        prompt = next(c for c in branch["config"]["conditions"] if c["matchstring"] == "^$")
        route = next(e for e in edges[branch["uid"]] if e.get("sourceoutputuid") == prompt["uid"])
        show = objects[route["destinationuid"]]
        self.assertEqual(show["type"], "alfred.workflow.utility.showalfred")
        self.assertEqual(show["config"]["argument"], "{var:search_keyword} ")
        self.assertFalse(show["config"]["leftcursor"])
