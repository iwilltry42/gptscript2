package loader

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/gptscript-ai/gptscript/pkg/types"

	"github.com/gptscript-ai/gptscript/pkg/openai"
	"github.com/hexops/autogold/v2"
	"github.com/stretchr/testify/require"
)

func toString(obj any) string {
	s, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		panic(err)
	}
	return string(s)
}

func TestIsOpenAPI(t *testing.T) {
	datav2, err := os.ReadFile("testdata/openapi_v2.json")
	require.NoError(t, err)
	v, ok := isOpenAPI(datav2)
	require.True(t, ok)
	require.Equal(t, 2, v, "expected openapi v2")

	datav3, err := os.ReadFile("testdata/openapi_v3.yaml")
	require.NoError(t, err)
	v, ok = isOpenAPI(datav3)
	require.True(t, ok)
	require.Equal(t, 3, v, "expected openapi v3")
}

func TestLoadOpenAPI(t *testing.T) {
	prg := types.Program{
		ToolSet: types.ToolSet{},
	}

	datav3, err := os.ReadFile("testdata/openapi_v3.yaml")
	require.NoError(t, err)
	_, err = readTool(context.Background(), nil, &prg, &source{Content: datav3}, "")
	require.NoError(t, err, "failed to read openapi v3")

	prg = types.Program{
		ToolSet: types.ToolSet{},
	}
	datav2, err := os.ReadFile("testdata/openapi_v2.json")
	require.NoError(t, err)
	_, err = readTool(context.Background(), nil, &prg, &source{Content: datav2}, "")
	require.NoError(t, err, "failed to read openapi v2")
}

func TestHelloWorld(t *testing.T) {
	prg, err := Program(context.Background(),
		"https://raw.githubusercontent.com/ibuildthecloud/test/bafe5a62174e8a0ea162277dcfe3a2ddb7eea928/example/sub/tool.gpt",
		"")
	require.NoError(t, err)
	autogold.Expect(strings.ReplaceAll(`{
  "name": "https://raw.githubusercontent.com/ibuildthecloud/test/bafe5a62174e8a0ea162277dcfe3a2ddb7eea928/example/sub/tool.gpt",
  "entryToolId": "https://raw.githubusercontent.com/ibuildthecloud/test/bafe5a62174e8a0ea162277dcfe3a2ddb7eea928/example/sub/tool.gpt:1",
  "toolSet": {
    "https://raw.githubusercontent.com/ibuildthecloud/test/bafe5a62174e8a0ea162277dcfe3a2ddb7eea928/example/bob.gpt:1": {
      "modelName": "MODEL",
      "internalPrompt": null,
      "instructions": "Say hello world",
      "id": "https://raw.githubusercontent.com/ibuildthecloud/test/bafe5a62174e8a0ea162277dcfe3a2ddb7eea928/example/bob.gpt:1",
      "localTools": {
        "": "https://raw.githubusercontent.com/ibuildthecloud/test/bafe5a62174e8a0ea162277dcfe3a2ddb7eea928/example/bob.gpt:1"
      },
      "source": {
        "location": "https://raw.githubusercontent.com/ibuildthecloud/test/bafe5a62174e8a0ea162277dcfe3a2ddb7eea928/example/bob.gpt",
        "lineNo": 1
      },
      "workingDir": "https://raw.githubusercontent.com/ibuildthecloud/test/bafe5a62174e8a0ea162277dcfe3a2ddb7eea928/example"
    },
    "https://raw.githubusercontent.com/ibuildthecloud/test/bafe5a62174e8a0ea162277dcfe3a2ddb7eea928/example/sub/tool.gpt:1": {
      "modelName": "MODEL",
      "internalPrompt": null,
      "tools": [
        "../bob.gpt"
      ],
      "instructions": "call bob",
      "id": "https://raw.githubusercontent.com/ibuildthecloud/test/bafe5a62174e8a0ea162277dcfe3a2ddb7eea928/example/sub/tool.gpt:1",
      "toolMapping": {
        "../bob.gpt": "https://raw.githubusercontent.com/ibuildthecloud/test/bafe5a62174e8a0ea162277dcfe3a2ddb7eea928/example/bob.gpt:1"
      },
      "localTools": {
        "": "https://raw.githubusercontent.com/ibuildthecloud/test/bafe5a62174e8a0ea162277dcfe3a2ddb7eea928/example/sub/tool.gpt:1"
      },
      "source": {
        "location": "https://raw.githubusercontent.com/ibuildthecloud/test/bafe5a62174e8a0ea162277dcfe3a2ddb7eea928/example/sub/tool.gpt",
        "lineNo": 1
      },
      "workingDir": "https://raw.githubusercontent.com/ibuildthecloud/test/bafe5a62174e8a0ea162277dcfe3a2ddb7eea928/example/sub"
    }
  }
}`, "MODEL", openai.DefaultModel)).Equal(t, toString(prg))

	prg, err = Program(context.Background(), "https://get.gptscript.ai/echo.gpt", "")
	require.NoError(t, err)

	autogold.Expect(strings.ReplaceAll(`{
  "name": "https://get.gptscript.ai/echo.gpt",
  "entryToolId": "https://get.gptscript.ai/echo.gpt:1",
  "toolSet": {
    "https://get.gptscript.ai/echo.gpt:1": {
      "description": "Returns back the input of the script",
      "modelName": "MODEL",
      "internalPrompt": null,
      "arguments": {
        "properties": {
          "input": {
            "description": "Any string",
            "type": "string"
          }
        },
        "type": "object"
      },
      "instructions": "echo \"${input}\"",
      "id": "https://get.gptscript.ai/echo.gpt:1",
      "localTools": {
        "": "https://get.gptscript.ai/echo.gpt:1"
      },
      "source": {
        "location": "https://get.gptscript.ai/echo.gpt",
        "lineNo": 1
      },
      "workingDir": "https://get.gptscript.ai/"
    }
  }
}`, "MODEL", openai.DefaultModel)).Equal(t, toString(prg))
}

func TestParse(t *testing.T) {
	tool, subTool := SplitToolRef("a from b with x")
	autogold.Expect([]string{"b", "a"}).Equal(t, []string{tool, subTool})

	tool, subTool = SplitToolRef("a with x")
	autogold.Expect([]string{"a", ""}).Equal(t, []string{tool, subTool})
}
