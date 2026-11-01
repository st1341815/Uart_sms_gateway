FROM python:3.11-alpine
WORKDIR /app
RUN pip install --no-cache-dir flask
COPY app.py /app/app.py
ENV FLASK_APP=app.py
EXPOSE 5000
CMD ["python","/app/app.py"]
