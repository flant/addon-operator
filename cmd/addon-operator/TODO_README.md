1. Хранить openapi values в values storage, потому что могут переопределиться дефолты
2. AppendValuesPatch для хранения с compaction
3. values хуков приоритетнее чем values из configMap